package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
)

type QuotaRefresher struct {
	cfg      Config
	store    *SafeStateStore
	client   *http.Client
	hostCall func(string, any) (json.RawMessage, error)
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewQuotaRefresher(cfg Config, store *SafeStateStore, hostCall func(string, any) (json.RawMessage, error)) *QuotaRefresher {
	return &QuotaRefresher{
		cfg:      cfg,
		store:    store,
		client:   &http.Client{Timeout: 15 * time.Second},
		hostCall: hostCall,
		stopCh:   make(chan struct{}),
	}
}

func (r *QuotaRefresher) Start() {
	r.wg.Add(1)
	go r.runLoop()
}

func (r *QuotaRefresher) Stop() {
	close(r.stopCh)
	r.wg.Wait()
}

func (r *QuotaRefresher) runLoop() {
	defer r.wg.Done()

	// Initial discovery
	r.refreshRoster(context.Background())

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.tick()
		}
	}
}

func (r *QuotaRefresher) tick() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Sync roster from host
	r.refreshRoster(ctx)

	// Identify accounts due for quota check
	accounts := r.store.List()
	now := time.Now().UTC()

	byProvider := make(map[string][]*AccountState)
	for _, acc := range accounts {
		if acc.Disabled {
			continue
		}
		byProvider[acc.Provider] = append(byProvider[acc.Provider], acc)
	}

	for provider, list := range byProvider {
		changed := false
		for _, acc := range list {
			if r.isDue(acc, now) {
				err := r.pollAccountQuota(ctx, acc)
				if err != nil {
					// Preserves prior state on failure
					if acc.Quota != nil {
						acc.Quota.LastError = err.Error()
					}
				} else {
					changed = true
				}
				r.store.Put(acc)
				time.Sleep(time.Duration(r.cfg.StaggerIntervalSec) * time.Second)
			}
		}

		if changed {
			r.recomputeAndApply(ctx, provider)
		}
	}
}

func (r *QuotaRefresher) isDue(acc *AccountState, now time.Time) bool {
	if acc.Quota == nil {
		return true
	}
	if acc.Quota.BackoffUntil != nil && now.Before(*acc.Quota.BackoffUntil) {
		return false
	}
	return now.After(acc.NextDueAt)
}

func (r *QuotaRefresher) refreshRoster(ctx context.Context) {
	if r.hostCall == nil {
		return
	}
	raw, err := r.hostCall(pluginabi.MethodHostAuthList, map[string]any{})
	if err != nil || len(raw) == 0 {
		return
	}

	var resp struct {
		Files []struct {
			ID        string         `json:"id"`
			AuthIndex string         `json:"auth_index"`
			Provider  string         `json:"provider"`
			Name      string         `json:"name"`
			Disabled  bool           `json:"disabled"`
			Priority  *int           `json:"priority"`
			Metadata  map[string]any `json:"metadata"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return
	}

	for _, f := range resp.Files {
		prov := strings.ToLower(strings.TrimSpace(f.Provider))
		if !isSupportedProvider(prov) {
			continue
		}

		existing, found := r.store.Get(f.ID)
		prio := 0
		if f.Priority != nil {
			prio = *f.Priority
		}

		if !found {
			acc := &AccountState{
				Key:             f.ID,
				AuthID:          f.ID,
				AuthIndex:       f.AuthIndex,
				Provider:        prov,
				Name:            f.Name,
				Alias:           AccountAlias(f.ID),
				Disabled:        f.Disabled,
				CurrentPriority: prio,
				NextDueAt:       time.Now().UTC(),
			}
			r.store.Put(acc)
		} else {
			existing.Disabled = f.Disabled
			existing.CurrentPriority = prio
			r.store.Put(existing)
		}
	}
}

func isSupportedProvider(p string) bool {
	switch p {
	case ProviderAntiGravity, ProviderCodex, ProviderClaude, ProviderKimi, ProviderXAI:
		return true
	default:
		return false
	}
}

func (r *QuotaRefresher) pollAccountQuota(ctx context.Context, acc *AccountState) error {
	// Query auth details from host to get token
	if r.hostCall == nil {
		return nil
	}
	rawAuth, err := r.hostCall(pluginabi.MethodHostAuthGet, map[string]any{"id": acc.AuthID})
	if err != nil {
		return err
	}
	var authObj struct {
		Token string         `json:"token"`
		Data  map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rawAuth, &authObj)

	token := authObj.Token
	if token == "" && authObj.Data != nil {
		if t, ok := authObj.Data["access_token"].(string); ok {
			token = t
		}
	}
	if token == "" {
		return fmt.Errorf("no access token found for %s", acc.AuthID)
	}

	req, err := buildQuotaRequest(ctx, acc.Provider, token)
	if err != nil {
		return err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		retrySec := 600 // default 10 minutes
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if s, err := strconv.Atoi(ra); err == nil && s > 0 {
				retrySec = s
			}
		}
		consecutive := 1
		if acc.Quota != nil {
			consecutive = acc.Quota.Consecutive429 + 1
		}
		// Exponential backoff
		multiplier := 1 << (consecutive - 1)
		if multiplier > 4 {
			multiplier = 4
		}
		until := time.Now().UTC().Add(time.Duration(retrySec*multiplier) * time.Second)
		if acc.Quota == nil {
			acc.Quota = &Quota{}
		}
		acc.Quota.BackoffUntil = &until
		acc.Quota.Consecutive429 = consecutive
		acc.Quota.LastError = "429 rate limited"
		acc.NextDueAt = until
		return nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("quota endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	quota, err := parseQuotaByProvider(acc.Provider, body)
	if err != nil {
		return err
	}

	quota.LastPolled = time.Now().UTC()
	quota.Consecutive429 = 0
	quota.BackoffUntil = nil
	acc.Quota = quota
	acc.NextDueAt = quota.LastPolled.Add(NextPollInterval(quota, r.cfg))
	return nil
}

func parseQuotaByProvider(provider string, body []byte) (*Quota, error) {
	switch provider {
	case ProviderAntiGravity:
		return ParseAntiGravityQuota(body)
	case ProviderClaude:
		return ParseClaudeQuota(body)
	case ProviderCodex:
		return ParseCodexQuota(body)
	case ProviderKimi:
		return ParseKimiQuota(body)
	case ProviderXAI:
		return ParseXAIQuota(body)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}

func buildQuotaRequest(ctx context.Context, provider, token string) (*http.Request, error) {
	var url string
	var method string = "GET"
	var body io.Reader

	switch provider {
	case ProviderAntiGravity:
		url = "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"
		method = "POST"
		body = bytes.NewReader([]byte("{}"))
	case ProviderClaude:
		url = "https://api.anthropic.com/api/oauth/usage"
	case ProviderCodex:
		url = "https://chatgpt.com/backend-api/wham/usage"
	case ProviderKimi:
		url = "https://api.kimi.com/coding/v1/usages"
	case ProviderXAI:
		url = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if provider == ProviderClaude {
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	} else if provider == ProviderCodex {
		req.Header.Set("User-Agent", "codex-tui/0.149.1")
	} else if provider == ProviderAntiGravity {
		req.Header.Set("User-Agent", "antigravity/cli/1.0.13")
	}

	return req, nil
}

func (r *QuotaRefresher) recomputeAndApply(ctx context.Context, provider string) {
	accounts := r.store.ListByProvider(provider)
	decisions := MakeDecisions(accounts, r.cfg)

	for _, d := range decisions {
		acc := d.Account
		acc.CurrentPriority = d.Priority
		acc.AssignedTier = d.Tier
		acc.DecisionReason = d.Reason
		acc.LastDecidedAt = time.Now().UTC()
		r.store.Put(acc)

		// Sync priority back to host if enabled
		if r.cfg.SyncToHostPriority && r.hostCall != nil {
			prio := d.Priority
			_, _ = r.hostCall(pluginabi.MethodHostAuthSave, map[string]any{
				"id":       acc.AuthID,
				"priority": prio,
			})
		}
	}
}
