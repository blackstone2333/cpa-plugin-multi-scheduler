# CPA Multi-Account Dynamic Orchestrator

English | [简体中文](README.zh-CN.md)

`cpa-plugin-multi-scheduler` (Plugin ID: `multi-account-orchestrator`) is a high-performance, native dynamic library plugin for [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI).

It provides real-time quota awareness, reset-window tier clustering, adaptive rate-limit protection, and health-based load balancing across multiple AI subscription accounts.

---

## Key Features

- **Multi-Provider Unified**: Native support for AntiGravity (Gemini), Codex (ChatGPT), Claude (Anthropic), Kimi (Moonshot), and xAI (Grok).
- **Reset-Window Tier Clustering**: Automatically clusters accounts with reset times within 16 hours into prioritized tiers (`400 / 300 / 200 / 100`) and round-robins requests within the top active tier.
- **Dynamic Quota Demotion**: Automatically demotes an account by one tier when its short-term (e.g. 5-hour) quota falls below 5%, shielding it from exhaustion.
- **Exhaustion Soft Sink**: When all long-period quota windows are exhausted, assigns priority `-1000` to prevent traffic routing without hard-disabling the account.
- **Adaptive Polling & Backoff**: Polls healthy accounts every 20 minutes; dynamically ramps up to 5m/3m when quotas run low; handles HTTP 429 with exponential backoff.
- **Built-in Visual Dashboard**: Dedicated Management UI and JSON status endpoint to monitor all credentials, tiers, remaining percentages, and reset countdowns in real time.

---

## Installation

### Method 1: CPA Plugin Store (Recommended)
Search for `multi-account-orchestrator` in the CPA Management Center Plugin Store and click **Install**.

### Method 2: Manual Installation
Download the platform archive from [GitHub Releases](https://github.com/blackstone2333/cpa-plugin-multi-scheduler/releases):
- Linux: `multi-account-orchestrator.so`
- macOS: `multi-account-orchestrator.dylib`
- Windows: `multi-account-orchestrator.dll`

Extract the library directly into your CPA `plugins/` directory.

---

## Configuration

Enable the plugin in CPA's `config.yaml`:

```yaml
plugins:
  enabled: true
  configs:
    multi-account-orchestrator:
      enabled: true
      tier_tolerance_hours: 16       # Reset window tolerance for clustering (hours)
      low_five_hour_threshold: 0.05  # 5h quota demotion threshold (5%)
      base_priority: 400            # Initial top tier priority
      priority_step: 100            # Step size per tier
      minimum_priority: 100         # Floor priority for active tiers
      exhausted_priority: -1000     # Negative sink priority for exhausted accounts
      poll_interval_high_min: 20    # Healthy quota check interval (minutes)
      poll_interval_medium_min: 5   # Medium quota check interval (minutes)
      poll_interval_low_min: 3      # Low quota check interval (minutes)
```

---

## Status Dashboard

Access the browser dashboard at:
```text
http://<your-cpa-host>:8317/v0/resource/plugins/multi-account-orchestrator/status
```
Or query via JSON API:
```text
GET /v0/management/plugins/multi-account-orchestrator/status?format=json
```

---

## License

[MIT License](LICENSE) © 2026 blackstone2333
