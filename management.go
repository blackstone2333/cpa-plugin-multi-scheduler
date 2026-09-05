package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type StatusView struct {
	Timestamp   string          `json:"timestamp"`
	Plugin      string          `json:"plugin"`
	Version     string          `json:"version"`
	TotalCounts int             `json:"total_accounts"`
	Accounts    []*AccountState `json:"accounts"`
}

type ManagementService struct {
	store *SafeStateStore
	cfg   Config
}

func NewManagementService(store *SafeStateStore, cfg Config) *ManagementService {
	return &ManagementService{store: store, cfg: cfg}
}

func (m *ManagementService) RegisterManagement(ctx context.Context, req pluginapi.ManagementRegistrationRequest) (pluginapi.ManagementRegistrationResponse, error) {
	statusRoute := pluginapi.ManagementRoute{
		Method:      "GET",
		Path:        req.BasePath + "/status",
		Description: "Multi-Account Orchestrator JSON Status API",
		Handler:     m,
	}

	uiRoute := pluginapi.ResourceRoute{
		Path:        req.ResourceBasePath + "/status",
		Menu:        "账号编排 (Orchestrator)",
		Description: "Multi-Account Dynamic Orchestrator Dashboard",
		Handler:     m,
	}

	return pluginapi.ManagementRegistrationResponse{
		Routes:    []pluginapi.ManagementRoute{statusRoute},
		Resources: []pluginapi.ResourceRoute{uiRoute},
	}, nil
}

func (m *ManagementService) HandleManagement(ctx context.Context, req pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	accounts := m.store.List()
	now := time.Now().UTC()

	view := StatusView{
		Timestamp:   now.Format(time.RFC3339),
		Plugin:      "cpa-plugin-multi-scheduler",
		Version:     "0.1.0",
		TotalCounts: len(accounts),
		Accounts:    accounts,
	}

	// If request accepts or targets JSON API
	if strings.HasPrefix(req.Path, "/v0/management/") || req.Query.Get("format") == "json" || strings.Contains(req.Headers.Get("Accept"), "application/json") {
		raw, _ := json.MarshalIndent(view, "", "  ")
		hdr := make(http.Header)
		hdr.Set("Content-Type", "application/json; charset=utf-8")
		return pluginapi.ManagementResponse{
			StatusCode: 200,
			Headers:    hdr,
			Body:       raw,
		}, nil
	}

	// Otherwise render modern clean HTML Dashboard
	html := m.renderHTML(view)
	hdr := make(http.Header)
	hdr.Set("Content-Type", "text/html; charset=utf-8")
	return pluginapi.ManagementResponse{
		StatusCode: 200,
		Headers:    hdr,
		Body:       []byte(html),
	}, nil
}

func (m *ManagementService) renderHTML(view StatusView) string {
	var rows strings.Builder
	for _, a := range view.Accounts {
		statusBadge := `<span class="badge badge-success">就绪 (Ready)</span>`
		if a.Disabled {
			statusBadge = `<span class="badge badge-muted">已停用 (Disabled)</span>`
		} else if a.Quota != nil && a.Quota.AllLongWindowsExhausted() {
			statusBadge = `<span class="badge badge-danger">额度耗尽/备用 (Exhausted/Standby)</span>`
		} else if a.CurrentPriority < 400 {
			statusBadge = `<span class="badge badge-warning">降级/次级 (Demoted)</span>`
		}

		fiveStr := "—"
		weekStr := "—"
		resetStr := "—"
		if a.Quota != nil {
			if a.Quota.FiveHourFraction != nil {
				fiveStr = fmt.Sprintf("%.1f%%", *a.Quota.FiveHourFraction*100)
			}
			weekStr = fmt.Sprintf("%.1f%%", a.Quota.WeeklyFraction*100)
			if !a.Quota.WeeklyReset.IsZero() {
				resetStr = a.Quota.WeeklyReset.Format("01-02 15:04")
			}
		}

		rows.WriteString(fmt.Sprintf(`
			<tr>
				<td><strong>%s</strong></td>
				<td><span class="provider-pill">%s</span></td>
				<td><span class="prio-tag">%d</span> (Tier %d)</td>
				<td>%s</td>
				<td>%s</td>
				<td>%s</td>
				<td>%s</td>
				<td class="text-muted small">%s</td>
			</tr>
		`, a.Alias, strings.ToUpper(a.Provider), a.CurrentPriority, a.AssignedTier, statusBadge, fiveStr, weekStr, resetStr, a.DecisionReason))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8">
	<title>CPA 账户动态编排调配 | Multi-Account Orchestrator</title>
	<style>
		:root {
			--bg: #0f172a;
			--surface: #1e293b;
			--border: #334155;
			--text: #f8fafc;
			--text-muted: #94a3b8;
			--primary: #38bdf8;
			--success: #10b981;
			--warning: #f59e0b;
			--danger: #ef4444;
		}
		body {
			margin: 0;
			padding: 24px;
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
			background: var(--bg);
			color: var(--text);
		}
		.container { max-width: 1100px; margin: 0 auto; }
		.header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; }
		h1 { margin: 0; font-size: 22px; font-weight: 600; color: var(--text); }
		.subtitle { color: var(--text-muted); font-size: 13px; margin-top: 4px; }
		.card { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
		table { width: 100%%; border-collapse: collapse; text-align: left; font-size: 13px; }
		th { background: #182234; padding: 12px 16px; color: var(--text-muted); font-weight: 500; border-bottom: 1px solid var(--border); }
		td { padding: 12px 16px; border-bottom: 1px solid var(--border); }
		tr:last-child td { border-bottom: none; }
		.badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 500; }
		.badge-success { background: rgba(16, 185, 129, 0.2); color: var(--success); }
		.badge-warning { background: rgba(245, 158, 11, 0.2); color: var(--warning); }
		.badge-danger { background: rgba(239, 68, 68, 0.2); color: var(--danger); }
		.badge-muted { background: rgba(148, 163, 184, 0.2); color: var(--text-muted); }
		.provider-pill { background: #334155; padding: 2px 6px; border-radius: 4px; font-size: 11px; font-weight: 600; }
		.prio-tag { font-family: monospace; font-weight: 700; color: var(--primary); }
		.small { font-size: 12px; }
		.text-muted { color: var(--text-muted); }
	</style>
</head>
<body>
	<div class="container">
		<div class="header">
			<div>
				<h1>CPA 账户动态编排调配 (Multi-Account Orchestrator)</h1>
				<div class="subtitle">实时额度感知、重置时间梯队聚合与健康度分流调度引擎</div>
			</div>
			<div class="subtitle">已托管账户：%d 个</div>
		</div>
		<div class="card">
			<table>
				<thead>
					<tr>
						<th>账号别名</th>
						<th>渠道</th>
						<th>编排优先级</th>
						<th>运行状态</th>
						<th>5h 剩余</th>
						<th>周额度剩余</th>
						<th>重置时间</th>
						<th>决策原因</th>
					</tr>
				</thead>
				<tbody>
					%s
				</tbody>
			</table>
		</div>
	</div>
</body>
</html>`, view.TotalCounts, rows.String())
}
