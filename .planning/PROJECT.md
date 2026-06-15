# chatlog_alpha

## What This Is

`chatlog_alpha` 是一个 Go 写的本地工具，让 macOS / Windows 用户访问 WeChat 4.x 本地聊天数据。提供 CLI（Cobra）、TUI（tview/tcell）、嵌入式 HTTP API（gin）+ MCP-style 访问、本地报告生成、语义检索、时间图谱处理、Obsidian 友好的图谱 digest 与 Hermes 推送。仓库把自身视作"AI 编码自动化的本地数据工具目标"，不是营销站点或通用 SaaS 后端。

## Core Value

> 经过 vNext 里程碑，chatlog_alpha 的本地知识图谱在常见故障（断电、磁盘满、Provider 不可用、模型错误内容、上游 WeChat 数据变更）下不丢、不漂、能持续被消费。

——如果其他一切都失败，**图谱数据完整且可被持续消费**这一件事必须成立。它驱动 vNext 在数据层 HA、Provider 切换、状态可观测三条线之间的取舍。

## Requirements

### Validated

下面这些能力已经存在并被日常依赖，写在 Validated 段只为让未来 GSD 阶段能引用为"现有约束"而不是"待办"。

- ✓ **Cobra CLI + TUI 入口** — `main.go` / `cmd/chatlog/root.go`，含 TUI 默认流与子命令派发（existing: `main.go`, `cmd/chatlog/root.go`）。
- ✓ **嵌入式 HTTP API + MCP 桥** — gin 路由 + MCP-style SSE/streamable server（existing: `internal/chatlog/http/{service,route,mcp}.go`）。
- ✓ **WeChat 本地数据访问** — 平台相关进程检测、密钥扫描、解密（v3/v4）、WCDB 兼容的 repository（existing: `internal/wechat/{process,key,decrypt}/`, `internal/wechatdb/`）。
- ✓ **多 Provider 语义检索** — Ollama 默认 + 可选 DeepSeek/GLM/MiniMax；`internal/chatlog/semantic/` 抽象 Embedding/Chat/Rerank 客户端（existing: `internal/chatlog/semantic/{client,manager,store}.go`）。
- ✓ **时间图谱** — 源队列、entity / fact / event / relation、timeline、graph QA、digester 渲染 Obsidian 风格 Markdown（existing: `internal/chatlog/temporalgraph/`）。
- ✓ **每日报告与消息钩子** — mention-based daily report 与 keyword 驱动的 MCP/Hermes/HTTP/POST 事件投递（existing: `internal/chatlog/dailyreport/`, `internal/chatlog/messagehook/`）。
- ✓ **Hermes 推送桥** — Weixin / QQ 通过本地 Hermes daemon 出站推送（existing: `internal/chatlog/hermespush/`，含 Python bridge 脚本）。
- ✓ **配置 / 密钥管理** — Viper 加载 + TUI/Server 两套配置 + 写入前脱敏（`datakey`/`imgkey`/`apikey`/`clientsecret`/`accesstoken`/`refreshtoken`/`password` 等键名正则匹配）（existing: `internal/chatlog/conf/`, `pkg/config/`）。
- ✓ **跨平台构建** — Go 1.24 + cgo，`.goreleaser.yaml` 与 GitHub Actions 同时构建 macOS arm64/amd64 与 Windows amd64（cgo）/ arm64（non-cgo）（existing: `.goreleaser.yaml`, `.github/workflows/release.yml`）。
- ✓ **Ralph 自动化与归档** — PRD→story→developer→validator 循环、bootstrap placeholder、归档目录、auto-merge 分支生命周期（existing: `scripts/ralph/`, `archive/`, `tasks/`，按 repo 约定不 git-track）。

### Active

vNext 3-6 个月里程碑要交付的能力。每个都是假设，落地后才能移到 Validated。

- [ ] **HA-WARM-1** — 图谱 worker / manager 在进程被 SIGTERM / OOM / panic 杀掉后能在 ≤ 60s 内由新进程续算：`pending`、`failed`、`last_processed_id` 三项从 SQLite 持久化状态恢复，不全量重跑已完成 source。
- [ ] **HA-WARM-2** — 同样的热重启窗口内，HTTP 路由 `/api/v1/graph/status` 持续可用；返回的 `running`、`source_count`、`processed`、`processing`、`pending`、`failed` 与断点前后数值连续（无归零抖动）。
- [ ] **HA-WARM-3** — 重启之间不出现"图谱队列被清空"或"上次处理成功的 evidence 落库失败导致重做"两种破坏性事件；通过检查点（每 N 条 source 或每 M 秒）与 SQLite 写事务保证持久边界。
- [ ] **OBS-1** — `/api/v1/graph/status` 补齐可观测指标：处理速率（`rate_per_min`）、最近一次成功时间戳（`last_success_at`）、最旧未处理 source 时间戳（`oldest_pending_at`）、磁盘使用（`db_size_bytes`）、Provider 错误分桶（`provider_error_buckets`）。
- [ ] **OBS-2** — 报警线：pending 增速过快（`rate_per_min` 长时间为 0 但 `pending` 上升）、failed 净增（5 分钟窗口净增 > N）、Provider 5xx 占比超阈、磁盘剩余 < 阈值、WeChat 库不可读。这些事件在 `graph_status` 响应里以 `alerts: []` 数组返回，并在 CLI / HTTP 长轮询里能即时看到。
- [ ] **PROV-1** — 5-key MiniMax 池之上：单 key 失效（HTTP 401/403/429/5xx）自动跳过到下一个 key，重试退避采用指数回退，封顶 5s。已实现 HA-001..HA-003 的能力不允许回归。
- [ ] **PROV-2** — 错误码分类扩展：把当前已知的 `1026`（input_sensitive）/ `1027`（output_sensitive）保留为"不重试"，并为 `4xx`（业务错误）/ `5xx`（上游故障）/ 网络层错误分别配置可重试 / 不可重试 / 退避时长。所有 provider（mmx、deepseek、glm、ollama）行为一致。
- [ ] **PROV-3** — 轮询 + 退避：所有 provider 失效切换走"轮询下一个可用 key / 下一个 provider"策略；5 个 key 全失效时入"全 provider 限流"态 → 30 分钟内不计入 `failed`（degraded 队列），30 分钟后才记一次 `failed` 并打 `alerts`。
- [ ] **PROV-4** — 为未来 MultiProviderRouter 留接口位：所有 provider 切换都经过一个 `ProviderRouter` 抽象；当前实现是"单 provider 内部多 key"，下一里程碑可平滑扩展为"多 provider 串联"。

### Out of Scope

- **不重写微信数据层** — `internal/wechat*` 与 `internal/wechatdb*` 已被验证成熟，vNext 不触碰；只读使用其能力。
- **不重排图谱 model / prompt** — 不切换基础模型，不重写 extraction/verification 提示模板；现有 prompt 走 `internal/chatlog/temporalgraph/helpers.go` 的 redact/crop 流程保持。
- **不重做消费层** — digest (`internal/chatlog/temporalgraph/digest*.go` + `cmd/chatlog/cmd_report.go`)、search、daily report、message hook 在 vNext 都不动；只服务它们所需的"图谱数据完整且持续可读"前提。
- **不做云端 / 分布式 HA** — 不引入远程依赖、不上 master/replica、不做 cross-host 复制；本地是唯一运行点。HA 边界 = 单机 + 单进程族。
- **不做新 provider 接入** — vNext 不再增加新模型供应商；只加固已有 4 家（mmx / deepseek / glm / ollama）的失效行为。
- **不做图谱 schema 改造** — 不改 entity / fact / event / relation 表结构；只加元数据列（如 `last_success_at`）与索引。
- **不做 UI 层 HA 可视化** — 不在 TUI 端加新面板；HA / OBS 能力对外只通过 `graph_status` HTTP 与 CLI 表现。

## Context

**技术环境（来自 `.planning/codebase/STACK.md` 与 `ARCHITECTURE.md`）：**

- Go 1.24.0 + cgo，主构建路径强依赖 `mattn/go-sqlite3`。
- 框架：cobra、viper、gin、tview/tcell、mark3labs/mcp-go、zerolog、mattn/go-sqlite3、gopsutil、fsnotify。
- 配置：env 前缀 `CHATLOG`，两套配置（TUI / Server）通过接口注入，敏感键在日志前脱敏。
- 部署：本地 CLI / 长时间运行的 HTTP daemon（默认 `0.0.0.0:5030`），桌面二进制走 GitHub Releases 跨平台分发。
- 持久化：本地 SQLite，单机单进程；`reports/` 符号链接到用户 Obsidian / 微信每日聊天记录目录，不进版本控制。

**已完成的 HA 基础（来自 `progress.md` 与 `scripts/ralph/progress.txt`）：**

- HA-001..HA-007 通过：chat runtime readiness、5-key 池可检视、失败源先分桶再 requeue、Provider 限流（1026/1027 不重试）、10 分钟单 source timeout、Prompt redact/crop、`chatlog serve --config-dir` 显式注入。
- Tail Completion 2026-06-06 / Config Drift 2026-06-07 / ollama_refused 2026-06-08 几次恢复形成"如何从烂摊子回到正轨"的剧本。
- `scripts/chatlog-ha-guard.sh` 已实现 env 注入与自愈，是运行时配套。

**已知风险（来自 `.planning/codebase/CONCERNS.md`）：**

- JSON serializer 风险：`SemanticConfig.APIKey` 字段当前只在 `gin.H` 投影里被屏蔽，未来如直接 `c.JSON(200, config)` 会泄露。
- 5-key 池硬编码 5，未来扩到 6+ 需新代码路径。
- SQLite WAL + busy_timeout 仍是 TODO（`TODOS.md`），与 vNext 的 HA-WARM-1 强相关——在写检查点前应先打开 WAL。
- 多次恢复都是"人肉 + 脚本"补回，缺自动化自愈，PROV-1/2/3 正是要把这一段固化为代码。
- Ralph 自动化本身（`scripts/ralph/ralph.py`）与 PRD 流程对 vNext 仍适用：每条 vNext 假设可拆为 Ralph story。

**用户身份**：

- 用户是传统电信运维工程师（`memory/user-identity-telecom-ops.md`）；不要把 `openai_prmpt.md` 的"AI 博主"人设写进任何 prompt / 模板 / 文档。

## Constraints

- **Tech stack**: Go 1.24 + cgo，主构建路径不可绕过 `mattn/go-sqlite3`；CGO=0 仅在 Windows arm64 实验性开启。
- **Local-only**: 任何 HA 方案不得引入网络依赖、远程服务、云端协调器；单进程 / 单 SQLite 文件。
- **Privacy boundary**: 验证必须靠路径 / 大小 / 时间戳 / 计数 / 状态码 / 错误分桶；不得打印聊天原文、digest body、模型输出。
- **Quota boundary**: 真实 provider 调用（mmx / deepseek / glm / ollama）默认不在验证里跑；mock-only 测试 + 一次性受控真跑 + 仅看 `summary_used` / `file_size` 字段。
- **Repository convention**: `scripts/ralph/`、`archive/`、`tasks/`、`.agents/`、`.claude/` 不进版本控制；`reports/`、`reports.backup-*`、`.cache/`、`logs/`、`outputs/`、`openai_prmpt.md` 永不入仓。
- **Test pyramid**: 单元测试用 stdlib `testing`；HTTP 层 mock-only；Provider 调用 mock-only + 受控真跑。
- **Release**: 任何对运行时行为的变更必须能 cross-build 出 darwin arm64/amd64 + windows amd64 四种产物（Windows arm64 不进 release）。

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Core Value = "图谱数据完整且可被持续消费" | 用户原话"完善知识图谱保证高可用"指向数据层 HA，而非消费层或 schema 扩张 | — Pending |
| 主线 = 数据层 HA（热重启 + 可观测 + Provider 切换） | 三项互为前置；只做热重启会让前 HA-007 的恢复方式继续靠人肉 | — Pending |
| 不重写微信数据层、不重排 model / prompt、不重做消费层 | vNext 范围必须可控；这些层成熟或已锁定为参照 | — Pending |
| 不做云端 / 分布式 HA | 单机单进程是产品形态本质；多节点会引入同步 / 隐私 / 部署复杂度 | — Pending |
| Provider 切换策略 = 轮询 + 退避（≤ 5s 指数） | 简单、可解释、回归容易；为未来 MultiProviderRouter 留接口 | — Pending |
| Provider 全失效 → degraded 队列（30 min 不计 failed） | 区分"全 provider 限流"与"个别 source 内容敏感"，避免被错误归类 | — Pending |
| `/api/v1/graph/status` 是 vNext 唯一可观测出口 | 现有 HTTP 路由已存在；扩字段 + 补 alerts 数组比另起服务便宜得多 | — Pending |
| 验收线 = 24h 压动 | failed 净增 ≤ 5、pending 下降、5 次重启均 ≤ 60s 续 | — Pending |
| PRD 工具 = Ralph（不改） | `scripts/ralph/ralph.py` + `prd.json` 已成熟；vNext 假设可拆为 stories | — Pending |

---

*Last updated: 2026-06-15 after brownfield init (`/gsd-new-project` Step 4)*

## Evolution

本文档随阶段 / 里程碑演进。

**每个阶段过渡（`/gsd-transition`）后：**

1. Active 假设已落地的 → 移到 Validated，注明 phase / version。
2. Active 假设被否决 / 推迟的 → 移到 Out of Scope，注明原因。
3. 新出现的 Active 假设 → 追加。
4. 决策需要追加 → 写入 Key Decisions，Outcome 标 ✓ / ⚠️ / — Pending。
5. "What This Is" 是否还准确 → 漂移就改。

**每个里程碑（`/gsd-complete-milestone`）后：**

1. 全段复审。
2. Core Value 是否仍是最高优先级？
3. Out of Scope 的排除理由是否仍成立？
4. Context 段（技术环境 / 已完成 HA 基础 / 风险）是否需要按当前真实状态更新。
