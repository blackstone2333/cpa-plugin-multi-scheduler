package main

import (
	"sort"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// GroupTiers groups accounts whose long-window reset times are within tolerance.
// Uses anchor-based clustering (non-transitive, stable).
func GroupTiers(accounts []*AccountState, tolerance time.Duration) [][]*AccountState {
	if len(accounts) == 0 {
		return nil
	}

	ordered := make([]*AccountState, len(accounts))
	copy(ordered, accounts)
	sort.Slice(ordered, func(i, j int) bool {
		rI := time.Time{}
		rJ := time.Time{}
		if ordered[i].Quota != nil {
			rI = ordered[i].Quota.WeeklyReset
		}
		if ordered[j].Quota != nil {
			rJ = ordered[j].Quota.WeeklyReset
		}
		if !rI.Equal(rJ) {
			return rI.Before(rJ)
		}
		return ordered[i].Key < ordered[j].Key
	})

	var tiers [][]*AccountState
	var anchors []time.Time

	for _, acc := range ordered {
		reset := time.Time{}
		if acc.Quota != nil {
			reset = acc.Quota.WeeklyReset
		}

		if len(tiers) == 0 || reset.Sub(anchors[len(anchors)-1]) > tolerance {
			tiers = append(tiers, []*AccountState{acc})
			anchors = append(anchors, reset)
		} else {
			tiers[len(tiers)-1] = append(tiers[len(tiers)-1], acc)
		}
	}

	return tiers
}

// Decision represents the computed priority outcome for one account.
type Decision struct {
	Account  *AccountState
	Priority int
	Tier     int
	Reason   string
}

// MakeDecisions calculates priorities and tiers for a list of accounts of one provider.
func MakeDecisions(accounts []*AccountState, cfg Config) []Decision {
	var exhausted []*AccountState
	var active []*AccountState

	for _, acc := range accounts {
		if acc.Disabled {
			continue
		}
		if acc.Quota != nil && acc.Quota.AllLongWindowsExhausted() {
			exhausted = append(exhausted, acc)
		} else {
			active = append(active, acc)
		}
	}

	tiers := GroupTiers(active, cfg.TierTolerance())
	scores := make([]int, len(tiers))
	for i := range tiers {
		score := cfg.BasePriority - i*cfg.PriorityStep
		if score < cfg.MinimumPriority {
			score = cfg.MinimumPriority
		}
		scores[i] = score
	}

	// CPA only selects the highest ready priority bucket.  If an entire earlier
	// reset tier is low on long-period quota, merge it into the next healthy tier
	// so the low account keeps receiving traffic and can drain to zero.  Accounts
	// already sharing a reset tier are never split apart.  A 5-hour-only dip does
	// not trigger a merge; CPA's own availability/cooldown handling covers that
	// temporary window.
	tierPriorities := append([]int(nil), scores...)
	tierLowOnly := make([]bool, len(tiers))
	for tierIdx, tier := range tiers {
		if len(tier) == 0 {
			continue
		}
		tierLowOnly[tierIdx] = true
		for _, acc := range tier {
			if !lowLongQuota(acc, cfg) {
				tierLowOnly[tierIdx] = false
				break
			}
		}
	}
	for tierIdx, lowOnly := range tierLowOnly {
		if !lowOnly {
			continue
		}
		for healthyIdx := tierIdx + 1; healthyIdx < len(tiers); healthyIdx++ {
			if !tierLowOnly[healthyIdx] {
				tierPriorities[tierIdx] = scores[healthyIdx]
				break
			}
		}
	}

	decisions := make([]Decision, 0, len(accounts))

	// Exhausted accounts assigned negative priority (-1000)
	for _, acc := range exhausted {
		decisions = append(decisions, Decision{
			Account:  acc,
			Priority: cfg.ExhaustedPriority,
			Tier:     -1,
			Reason:   "all long quota windows exhausted",
		})
	}

	// Active tiers
	for tierIdx, tier := range tiers {
		tierScore := scores[tierIdx]
		for _, acc := range tier {
			priority := tierPriorities[tierIdx]
			reason := "long-window reset order"
			if priority != tierScore {
				reason = "low-only tier merged with next healthy tier"
			}

			decisions = append(decisions, Decision{
				Account:  acc,
				Priority: priority,
				Tier:     tierIdx,
				Reason:   reason,
			})
		}
	}

	sort.Slice(decisions, func(i, j int) bool {
		return decisions[i].Account.Key < decisions[j].Account.Key
	})

	return decisions
}

// lowLongQuota reports whether the account's smallest observed long-period
// remainder is at or below the configured threshold.  Missing quota is not low:
// an account must be successfully observed before it can be merged.
func lowLongQuota(acc *AccountState, cfg Config) bool {
	if acc == nil || acc.Quota == nil {
		return false
	}
	longs := acc.Quota.LongFractions()
	if len(longs) == 0 {
		return false
	}
	minimum := longs[0]
	for _, value := range longs[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return minimum <= cfg.LowFiveHourThreshold
}

// NextPollInterval calculates when to check quota next based on remaining percentage.
func NextPollInterval(q *Quota, cfg Config) time.Duration {
	if q == nil {
		return time.Duration(cfg.PollIntervalMediumMin) * time.Minute
	}
	frac := q.PollFraction()
	if frac > 0.15 {
		return time.Duration(cfg.PollIntervalHighMin) * time.Minute
	}
	if frac >= 0.05 {
		return time.Duration(cfg.PollIntervalMediumMin) * time.Minute
	}
	return time.Duration(cfg.PollIntervalLowMin) * time.Minute
}

// SelectBestAccount implements the priority-tiered round-robin candidate selection.
func SelectBestAccount(candidates []pluginapi.SchedulerAuthCandidate, provider string, store *SafeStateStore) (string, bool) {
	if len(candidates) == 0 {
		return "", false
	}

	type candidateInfo struct {
		candidate pluginapi.SchedulerAuthCandidate
		state     *AccountState
		priority  int
	}

	var infos []candidateInfo
	for _, c := range candidates {
		state, found := store.Get(c.ID)
		prio := c.Priority
		if found {
			prio = state.CurrentPriority
		}
		infos = append(infos, candidateInfo{
			candidate: c,
			state:     state,
			priority:  prio,
		})
	}

	// Filter out hard exhausted accounts (prio <= -1000) if any non-exhausted candidate exists
	var nonExhausted []candidateInfo
	for _, inf := range infos {
		if inf.priority > -1000 {
			nonExhausted = append(nonExhausted, inf)
		}
	}

	targetPool := nonExhausted
	if len(targetPool) == 0 {
		// All candidates are exhausted or unassigned, consider all candidates
		targetPool = infos
	}

	// Find the highest priority present in targetPool
	maxPrio := targetPool[0].priority
	for _, inf := range targetPool[1:] {
		if inf.priority > maxPrio {
			maxPrio = inf.priority
		}
	}

	// Collect all candidates sharing the highest priority
	var topTier []candidateInfo
	for _, inf := range targetPool {
		if inf.priority == maxPrio {
			topTier = append(topTier, inf)
		}
	}

	// Stable sort by ID before round robin to ensure determinism
	sort.Slice(topTier, func(i, j int) bool {
		return topTier[i].candidate.ID < topTier[j].candidate.ID
	})

	// Round-robin selection inside the top tier
	pickedIdx := store.NextRR(provider, len(topTier))
	return topTier[pickedIdx].candidate.ID, true
}
