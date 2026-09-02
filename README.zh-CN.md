# CPA账户动态编排调配 (Multi-Account Dynamic Orchestrator)

[English](README.md) | 简体中文

`cpa-plugin-multi-scheduler`（插件 ID: `multi-account-orchestrator`）是专为 [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI) 打造的多渠道多账号配额感知动态编排插件。

通过实时感知各账号的周期剩余额度与重置时间，自动将账号聚合为梯队分流组，实现高并发平滑轮询、低额度自动降级与长周期耗尽软下沉，最大化利用每一份订阅额度。

---

## 核心特性

- **多渠道全覆盖**：同时支持 AntiGravity (Gemini)、Codex (ChatGPT)、Claude (Anthropic)、Kimi (Moonshot) 与 xAI (Grok)。
- **重置梯队聚合**：将重置时间相差在 16 小时以内的账号智能归入同一优先级梯队（400 / 300 / 200 / 100），同梯队内自动进行轮询负载均衡。
- **动态额度保护**：当账号的短期（如 5 小时）额度低于 5% 时，主动降权一档，将负载引流至充足账号。
- **耗尽软隔离**：长周期额度全部耗尽时，赋予 -1000 优先级软下沉，绝不物理禁用账号，待额度恢复自动回血复出。
- **全自动自适应巡检**：健康账号 20 分钟检查一次，中低额度 5/3 分钟动态加速巡检；遇 429 速率限制自动指数退避。
- **原生可视化仪表盘**：内置专属 Web 状态看板与 JSON 监控接口，随时查看各账号健康度与梯队。

---

## 安装方式

### 方式一：CPA 官方插件商店一键安装（推荐）
在 CLIProxyAPI 管理后台的 **插件市场 (Plugin Store)** 中搜索 `multi-account-orchestrator` 或 `CPA账户动态编排调配`，点击一键安装即可。

### 方式二：手动安装
从 [GitHub Releases](https://github.com/blackstone2333/cpa-plugin-multi-scheduler/releases) 下载适合您系统架构的压缩包：
- Linux: `multi-account-orchestrator.so`
- macOS: `multi-account-orchestrator.dylib`
- Windows: `multi-account-orchestrator.dll`

解压后放置于 CPA 的 `plugins/` 相应系统目录即可。

---

## 配置说明

在 CPA 的 `config.yaml` 中启用插件：

```yaml
plugins:
  enabled: true
  configs:
    multi-account-orchestrator:
      enabled: true
      tier_tolerance_hours: 16       # 重置时间容差窗口（小时）
      low_five_hour_threshold: 0.05  # 5小时额度降级阈值（5%）
      base_priority: 400            # 最高初始优先级
      priority_step: 100            # 梯队降级步长
      minimum_priority: 100         # 最低可用优先级
      exhausted_priority: -1000     # 耗尽下沉优先级
      poll_interval_high_min: 20    # 充裕额度巡检间隔（分钟）
      poll_interval_medium_min: 5   # 中等额度巡检间隔（分钟）
      poll_interval_low_min: 3      # 临界额度巡检间隔（分钟）
```

---

## 可视化看板

安装并启动后，访问 CPA 资源端点即可进入可视化看板：
```text
http://<你的CPA地址>:8317/v0/resource/plugins/multi-account-orchestrator/status
```
或通过 API 查询实时状态：
```text
GET /v0/management/plugins/multi-account-orchestrator/status?format=json
```

---

## 开源协议

本项目采用 [MIT License](LICENSE) 许可。
