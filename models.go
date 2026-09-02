package main

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Supported providers
const (
	ProviderAntiGravity = "antigravity"
	ProviderCodex       = "codex"
	ProviderClaude      = "claude"
	ProviderKimi        = "kimi"
	ProviderXAI         = "xai"
)

// Quota holds normalized quota metrics for an account.
type Quota struct {
	FiveHourFraction *float64   `json:"five_hour_fraction,omitempty"`
	WeeklyFraction   float64    `json:"weekly_fraction"`
	WeeklyReset      time.Time  `json:"weekly_reset"`
	WeeklyFractions  []float64  `json:"weekly_fractions,omitempty"`
	LastPolled       time.Time  `json:"last_polled"`
	LastError        string     `json:"last_error,omitempty"`
	BackoffUntil     *time.Time `json:"backoff_until,omitempty"`
	Consecutive429   int        `json:"consecutive_429"`
}

func (q *Quota) LongFractions() []float64 {
	if len(q.WeeklyFractions) > 0 {
		return q.WeeklyFractions
	}
	return []float64{q.WeeklyFraction}
}

func (q *Quota) PollFraction() float64 {
	longs := q.LongFractions()
	minVal := 1.0
	for _, v := range longs {
		if v < minVal {
			minVal = v
		}
	}
	if q.FiveHourFraction != nil && *q.FiveHourFraction < minVal {
		minVal = *q.FiveHourFraction
	}
	return minVal
}

func (q *Quota) AllLongWindowsExhausted() bool {
	longs := q.LongFractions()
	if len(longs) == 0 {
		return false
	}
	for _, v := range longs {
		if v > 0.0 {
			return false
		}
	}
	return true
}

// AccountState represents the tracked state of one credential.
type AccountState struct {
	Key             string    `json:"key"`
	AuthID          string    `json:"auth_id"`
	AuthIndex       string    `json:"auth_index"`
	Provider        string    `json:"provider"`
	Name            string    `json:"name"`
	Alias           string    `json:"alias"`
	Disabled        bool      `json:"disabled"`
	CurrentPriority int       `json:"current_priority"`
	AssignedTier    int       `json:"assigned_tier"`
	Quota           *Quota    `json:"quota,omitempty"`
	NextDueAt       time.Time `json:"next_due_at"`
	LastDecidedAt   time.Time `json:"last_decided_at"`
	DecisionReason  string    `json:"decision_reason"`
}

// AccountAlias returns a stable pseudonymous display alias (e.g. AC-a1b2c3d4).
func AccountAlias(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "AC-" + hex.EncodeToString(sum[:])[:8]
}

// SafeStateStore manages concurrency-safe in-memory account states.
type SafeStateStore struct {
	mu       sync.RWMutex
	accounts map[string]*AccountState
	rrIndex  map[string]int // provider -> round-robin pointer
}

func NewSafeStateStore() *SafeStateStore {
	return &SafeStateStore{
		accounts: make(map[string]*AccountState),
		rrIndex:  make(map[string]int),
	}
}

func (s *SafeStateStore) Get(authID string) (*AccountState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[authID]
	if !ok {
		return nil, false
	}
	copied := *a
	return &copied, true
}

func (s *SafeStateStore) Put(acc *AccountState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := *acc
	s.accounts[acc.AuthID] = &copied
}

func (s *SafeStateStore) List() []*AccountState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*AccountState, 0, len(s.accounts))
	for _, a := range s.accounts {
		copied := *a
		list = append(list, &copied)
	}
	return list
}

func (s *SafeStateStore) ListByProvider(provider string) []*AccountState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*AccountState
	for _, a := range s.accounts {
		if a.Provider == provider {
			copied := *a
			list = append(list, &copied)
		}
	}
	return list
}

func (s *SafeStateStore) NextRR(provider string, count int) int {
	if count <= 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.rrIndex[provider]
	next := (idx + 1) % count
	s.rrIndex[provider] = next
	return idx % count
}
