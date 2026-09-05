package main

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestGroupTiers(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	accounts := []*AccountState{
		{
			Key: "acc1",
			Quota: &Quota{
				WeeklyReset: now,
			},
		},
		{
			Key: "acc2",
			Quota: &Quota{
				WeeklyReset: now.Add(10 * time.Hour), // within 16h of acc1 -> same tier
			},
		},
		{
			Key: "acc3",
			Quota: &Quota{
				WeeklyReset: now.Add(25 * time.Hour), // >16h -> new tier
			},
		},
	}

	tiers := GroupTiers(accounts, 16*time.Hour)
	if len(tiers) != 2 {
		t.Fatalf("expected 2 tiers, got %d", len(tiers))
	}
	if len(tiers[0]) != 2 {
		t.Errorf("expected 2 accounts in tier 0, got %d", len(tiers[0]))
	}
	if len(tiers[1]) != 1 {
		t.Errorf("expected 1 account in tier 1, got %d", len(tiers[1]))
	}
}

func TestMakeDecisions(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	fiveNormal := 0.50

	accounts := []*AccountState{
		{
			Key: "acc-healthy",
			Quota: &Quota{
				FiveHourFraction: &fiveNormal,
				WeeklyFraction:   0.8,
				WeeklyReset:      now,
			},
		},
		{
			Key: "acc-low-long",
			Quota: &Quota{
				FiveHourFraction: &fiveNormal,
				WeeklyFraction:   0.03, // low long-period remainder
				WeeklyReset:      now.Add(24 * time.Hour),
			},
		},
		{
			Key: "acc-healthy-later",
			Quota: &Quota{
				FiveHourFraction: &fiveNormal,
				WeeklyFraction:   0.8,
				WeeklyReset:      now.Add(48 * time.Hour),
			},
		},
		{
			Key: "acc-exhausted",
			Quota: &Quota{
				FiveHourFraction: &fiveNormal,
				WeeklyFraction:   0.0,
				WeeklyFractions:  []float64{0.0, 0.0},
				WeeklyReset:      now,
			},
		},
	}

	cfg := DefaultConfig()
	decisions := MakeDecisions(accounts, cfg)

	decMap := make(map[string]Decision)
	for _, d := range decisions {
		decMap[d.Account.Key] = d
	}

	if d, ok := decMap["acc-healthy"]; !ok || d.Priority != 400 {
		t.Errorf("acc-healthy priority = %d, want 400", d.Priority)
	}
	if d, ok := decMap["acc-low-long"]; !ok || d.Priority != 200 {
		t.Errorf("acc-low-long priority = %d, want 200 (merged with next healthy tier)", d.Priority)
	}
	if d, ok := decMap["acc-healthy-later"]; !ok || d.Priority != 200 {
		t.Errorf("acc-healthy-later priority = %d, want 200", d.Priority)
	}
	if d, ok := decMap["acc-exhausted"]; !ok || d.Priority != 0 {
		t.Errorf("acc-exhausted priority = %d, want 0", d.Priority)
	}
}

func TestMakeDecisionsKeepsLowAccountWithHealthyPeerInSameTier(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	five := 0.8
	accounts := []*AccountState{
		{Key: "low", Quota: &Quota{FiveHourFraction: &five, WeeklyFraction: 0.04, WeeklyReset: now}},
		{Key: "healthy", Quota: &Quota{FiveHourFraction: &five, WeeklyFraction: 0.90, WeeklyReset: now.Add(2 * time.Hour)}},
		{Key: "later", Quota: &Quota{FiveHourFraction: &five, WeeklyFraction: 0.90, WeeklyReset: now.Add(48 * time.Hour)}},
	}
	decisions := MakeDecisions(accounts, DefaultConfig())
	got := make(map[string]int)
	for _, decision := range decisions {
		got[decision.Account.Key] = decision.Priority
	}
	if got["low"] != 400 || got["healthy"] != 400 || got["later"] != 300 {
		t.Fatalf("priorities = %#v, want low/healthy=400 and later=300", got)
	}
}

func TestMakeDecisionsDoesNotMergeFiveHourOnlyDip(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	fiveLow := 0.03
	fiveHealthy := 0.8
	accounts := []*AccountState{
		{Key: "short-low", Quota: &Quota{FiveHourFraction: &fiveLow, WeeklyFraction: 0.8, WeeklyReset: now}},
		{Key: "healthy-later", Quota: &Quota{FiveHourFraction: &fiveHealthy, WeeklyFraction: 0.8, WeeklyReset: now.Add(48 * time.Hour)}},
	}
	decisions := MakeDecisions(accounts, DefaultConfig())
	got := make(map[string]int)
	for _, decision := range decisions {
		got[decision.Account.Key] = decision.Priority
	}
	if got["short-low"] != 400 || got["healthy-later"] != 300 {
		t.Fatalf("priorities = %#v, want short-low=400 and healthy-later=300", got)
	}
}

func TestMakeDecisionsOnlySinksWhenEveryLongWindowIsExhausted(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	five := 0.8
	accounts := []*AccountState{
		{
			Key: "partially-exhausted",
			Quota: &Quota{
				FiveHourFraction: &five,
				WeeklyFraction:   0,
				WeeklyFractions:  []float64{0, 0.04},
				WeeklyReset:      now,
			},
		},
		{
			Key: "fully-exhausted",
			Quota: &Quota{
				FiveHourFraction: &five,
				WeeklyFraction:   0,
				WeeklyFractions:  []float64{0, 0},
				WeeklyReset:      now,
			},
		},
	}

	decisions := MakeDecisions(accounts, DefaultConfig())
	got := make(map[string]int)
	for _, decision := range decisions {
		got[decision.Account.Key] = decision.Priority
	}
	if got["partially-exhausted"] != 400 {
		t.Fatalf("partially-exhausted priority = %d, want 400 while one long window remains", got["partially-exhausted"])
	}
	if got["fully-exhausted"] != 0 {
		t.Fatalf("fully-exhausted priority = %d, want 0", got["fully-exhausted"])
	}
}

func TestMakeDecisionsKeepsOnlyEnabledExhaustedAccountFullyRoutable(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	decisions := MakeDecisions([]*AccountState{{
		Key: "only",
		Quota: &Quota{
			WeeklyFraction:  0,
			WeeklyFractions: []float64{0, 0},
			WeeklyReset:     now,
		},
	}}, DefaultConfig())
	if len(decisions) != 1 || decisions[0].Priority != 400 {
		t.Fatalf("only enabled account decision = %#v, want priority 400", decisions)
	}
}

func TestMakeDecisionsClampsLegacyNegativeExhaustedPriority(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cfg := DefaultConfig()
	cfg.ExhaustedPriority = -1000
	decisions := MakeDecisions([]*AccountState{
		{Key: "empty", Quota: &Quota{WeeklyFraction: 0, WeeklyFractions: []float64{0}, WeeklyReset: now}},
		{Key: "healthy", Quota: &Quota{WeeklyFraction: 1, WeeklyFractions: []float64{1}, WeeklyReset: now}},
	}, cfg)
	for _, decision := range decisions {
		if decision.Priority < 0 {
			t.Fatalf("legacy config produced negative priority: %#v", decision)
		}
	}
}

func TestSelectBestAccountRoundRobin(t *testing.T) {
	store := NewSafeStateStore()
	store.Put(&AccountState{
		AuthID:          "auth-1",
		CurrentPriority: 400,
	})
	store.Put(&AccountState{
		AuthID:          "auth-2",
		CurrentPriority: 400,
	})
	store.Put(&AccountState{
		AuthID:          "auth-exhausted",
		CurrentPriority: 0,
	})

	candidates := []pluginapi.SchedulerAuthCandidate{
		{ID: "auth-1", Priority: 400},
		{ID: "auth-2", Priority: 400},
		{ID: "auth-exhausted", Priority: 0},
	}

	// First call
	id1, ok1 := SelectBestAccount(candidates, "codex", store)
	if !ok1 || (id1 != "auth-1" && id1 != "auth-2") {
		t.Fatalf("unexpected picked id: %s", id1)
	}

	// Second call should alternate (round-robin)
	id2, ok2 := SelectBestAccount(candidates, "codex", store)
	if !ok2 || (id2 != "auth-1" && id2 != "auth-2") {
		t.Fatalf("unexpected picked id: %s", id2)
	}
	if id1 == id2 {
		t.Errorf("expected round-robin alternation between auth-1 and auth-2, got %s twice", id1)
	}
}

func TestSelectBestAccountAllowsOnlyZeroPriorityCandidate(t *testing.T) {
	store := NewSafeStateStore()
	store.Put(&AccountState{AuthID: "only", CurrentPriority: 0})
	id, ok := SelectBestAccount(
		[]pluginapi.SchedulerAuthCandidate{{ID: "only", Priority: 0}},
		"codex",
		store,
	)
	if !ok || id != "only" {
		t.Fatalf("picked (%q, %v), want only zero-priority candidate", id, ok)
	}
}

func TestParsers(t *testing.T) {
	// AntiGravity test
	agRaw := []byte(`{
		"groups": [
			{
				"displayName": "Gemini 2.5 Pro",
				"buckets": [
					{"window": "5h", "remainingFraction": 0.45},
					{"window": "weekly", "remainingFraction": 0.85, "resetTime": "2026-09-10T12:00:00Z"}
				]
			}
		]
	}`)
	agQuota, err := ParseAntiGravityQuota(agRaw)
	if err != nil {
		t.Fatalf("ParseAntiGravityQuota: %v", err)
	}
	if agQuota.FiveHourFraction == nil || *agQuota.FiveHourFraction != 0.45 {
		t.Errorf("expected 5h fraction 0.45, got %v", agQuota.FiveHourFraction)
	}
	if agQuota.WeeklyFraction != 0.85 {
		t.Errorf("expected weekly fraction 0.85, got %f", agQuota.WeeklyFraction)
	}

	// Claude test
	claudeRaw := []byte(`{
		"five_hour": {"utilization": 20.0, "resets_at": 1788888888},
		"seven_day": {"utilization": 40.0, "resets_at": 1789999999}
	}`)
	clQuota, err := ParseClaudeQuota(claudeRaw)
	if err != nil {
		t.Fatalf("ParseClaudeQuota: %v", err)
	}
	if clQuota.FiveHourFraction == nil || *clQuota.FiveHourFraction != 0.80 {
		t.Errorf("expected claude 5h remaining 0.80, got %v", clQuota.FiveHourFraction)
	}
	if clQuota.WeeklyFraction != 0.60 {
		t.Errorf("expected claude 7d remaining 0.60, got %f", clQuota.WeeklyFraction)
	}

	// Codex test
	codexRaw := []byte(`{
		"rate_limit": {
			"primary_window": {
				"limit_window_seconds": 18000,
				"used_percent": 10.0,
				"reset_at": "2026-09-04T00:00:00Z"
			},
			"secondary_window": {
				"limit_window_seconds": 604800,
				"used_percent": 25.0,
				"reset_at": "2026-09-10T00:00:00Z"
			}
		}
	}`)
	cxQuota, err := ParseCodexQuota(codexRaw)
	if err != nil {
		t.Fatalf("ParseCodexQuota: %v", err)
	}
	if cxQuota.FiveHourFraction == nil || *cxQuota.FiveHourFraction != 0.90 {
		t.Errorf("expected codex 5h remaining 0.90, got %v", cxQuota.FiveHourFraction)
	}
	if cxQuota.WeeklyFraction != 0.75 {
		t.Errorf("expected codex weekly remaining 0.75, got %f", cxQuota.WeeklyFraction)
	}
}

func TestParseCodexQuotaDoesNotLetAuxiliaryModelQuotaMaskMainExhaustion(t *testing.T) {
	raw := []byte(`{
		"rate_limit": {
			"primary_window": {
				"limit_window_seconds": 18000,
				"used_percent": 100.0,
				"reset_at": "2026-09-04T05:00:00Z"
			},
			"secondary_window": {
				"limit_window_seconds": 604800,
				"used_percent": 100.0,
				"reset_at": "2026-09-10T00:00:00Z"
			}
		},
		"additional_rate_limits": [{
			"limit_name": "GPT-5.3-Codex-Spark",
			"rate_limit": {
				"primary_window": {
					"limit_window_seconds": 18000,
					"used_percent": 0.0,
					"reset_at": "2026-09-04T05:00:00Z"
				},
				"secondary_window": {
					"limit_window_seconds": 604800,
					"used_percent": 99.0,
					"reset_at": "2026-09-11T00:00:00Z"
				}
			}
		}]
	}`)
	quota, err := ParseCodexQuota(raw)
	if err != nil {
		t.Fatalf("ParseCodexQuota: %v", err)
	}
	if quota.WeeklyFraction != 0 || !quota.AllLongWindowsExhausted() {
		t.Fatalf("main quota should be exhausted, got weekly=%v long=%v", quota.WeeklyFraction, quota.LongFractions())
	}
}
