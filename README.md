# CPA Multi-Account Dynamic Orchestrator

English | [简体中文](README.zh-CN.md)

`cpa-plugin-multi-scheduler` (Plugin ID: `multi-account-orchestrator`) is a high-performance, native dynamic library plugin for [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI).

It provides real-time quota awareness, reset-window tier clustering, adaptive rate-limit protection, and health-based load balancing across multiple AI subscription accounts.

---

## Key Features

- **Multi-Provider Unified**: Native support for AntiGravity (Gemini), Codex (ChatGPT), Claude (Anthropic), Kimi (Moonshot), and xAI (Grok).
- **Reset-Window Tier Clustering**: Automatically clusters accounts with reset times within 16 hours into prioritized tiers (`400 / 300 / 200 / 100`) and round-robins requests within the top active tier.
- **Low-Quota Drain**: When every account in an earlier reset tier is at or below 5% on its long-period quota, that tier merges with the next healthy tier. Positive-quota accounts remain equally routable until all long windows are exhausted.
- **Short-Window Tolerance**: A temporary 5-hour dip does not change priority; CPA's own availability and cooldown handling cover that window.
- **Exhaustion Last Resort**: With multiple enabled credentials, an exhausted account falls to priority `0` but remains callable. If it is the provider's only enabled credential, it stays at priority `400`; the plugin never hard-disables it.
- **Codex Main-Quota Routing**: Codex scheduling follows the main account quota; model-specific auxiliary limits such as Spark and Code Review cannot keep ordinary Codex routing active after the main weekly quota reaches zero.
- **Adaptive Polling & Backoff**: Polls accounts above 15% every 30 minutes, 5%–15% every 15 minutes, and below 5% every 10 minutes; handles HTTP 429 with exponential backoff.
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
      low_five_hour_threshold: 0.05  # Legacy key: long-period merge threshold (5%)
      base_priority: 400            # Initial top tier priority
      priority_step: 100            # Step size per tier
      minimum_priority: 100         # Floor priority for active tiers
      exhausted_priority: 0         # Callable last-resort priority for exhausted accounts
      poll_interval_high_min: 30    # Healthy quota check interval (minutes)
      poll_interval_medium_min: 15   # Medium quota check interval (minutes)
      poll_interval_low_min: 10      # Low quota check interval (minutes)
```

Legacy negative `exhausted_priority` values are accepted but clamped to `0` so an upgrade cannot leave an account unroutable.

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
