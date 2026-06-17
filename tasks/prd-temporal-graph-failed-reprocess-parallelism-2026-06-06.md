# PRD: Temporal Graph Failed Sources Reprocess & Parallelism to 5

## 1) 介绍 / 概述

当前 temporal graph 运行中存在大量失败源（failure bucket）未完成重跑，且 MiniMax 已具备 5 个 API Key（可并行 5 份调用）。目标是在不暴露私聊内容的前提下，增加处理能力到 5 并发，重新拉起失败任务，降低 `failed` 池并持续监控是否出现雪崩错误。

## 2) 目标

- 在不清理已有图谱数据（实体/关系/事件/事实）的前提下，完成失败源的可恢复重试。
- 支持将 `graph` worker 与 `enqueue` worker 一起提升到 `5/5`。
- 重试策略只自动重投可恢复错误，不误重试明显不可恢复错误。
- 建立可执行、可复盘的运行闭环：重试前后状态可比对、错误率可回滚。

## 3) User Stories

### US-001: 一键恢复可恢复失败源
**描述：** 作为运维执行者，我想要通过现有 API 把可恢复失败源重新入队，以便持续拉起图谱构建任务。

**Acceptance Criteria：**
- [ ] 通过 `POST /api/v1/graph/rebuild?format=json`（`body`: `{\"reset\": false}` 或 query `reset=false`）触发重建时，返回值包含 `ok=true` 和 `status`。
- [ ] 记录触发前的 `failed` 数量与 `pending` 数量。
- [ ] 触发后 30 秒内再次查询 `GET /api/v1/graph/status?format=json`，能观察到：`failed` 非增，`pending` 增加且 `source_count` 不下降。
- [ ] 如果失败主要为“配置未就绪/模型超时”类，可被恢复机制重新入队；其他明显不可恢复类错误不会被误清空。
- [ ] 全程不请求任何私聊正文，只读 `status` 与计数。

### US-002: 并行度提升到 5 并保持可回滚
**描述：** 作为执行者，我想要把运行参数调整到 5 并发，以便提升吞吐；同时能在失败率异常时快速回落。

**Acceptance Criteria：**
- [ ] 调用 `POST /api/v1/graph/config?format=json` 提交 `{"workers":5,"enqueue_workers":5}` 后，返回状态和再次查询的 `GET /api/v1/graph/config?format=json` 均显示 `workers=5`、`enqueue_workers=5`。
- [ ] 应用配置后，`GET /api/v1/graph/status?format=json` 显示 `workers=5` 与 `enqueue_workers=5`。
- [ ] 在 3 分钟观察窗内记录 `processing_rate_per_minute`，若显著高于基线则保留并发；若 `failed` 和 `last_error` 连续上升则降级为 `workers=3`、`enqueue_workers=3`，并在 `progress` 中记录。
- [ ] 回滚动作只涉及 `POST /api/v1/graph/config`，不重建数据库，不删除私有数据。

### US-003: 可复现的失败分桶验证
**描述：** 作为运维分析者，我想要在每次重跑前后记录失败桶和处理桶分布，以判断是否继续扩并发。

**Acceptance Criteria：**
- [ ] 使用只读 SQLite 查询统计 `graph_source_records` 的失败分布（按 `status='failed'` 及错误关键字计数）。
- [ ] 重跑前后均留存 `failed` 总数、`pending` 总数、`processed` 总数快照，写入本地运行日志。
- [ ] 至少一次 `POST /api/v1/graph/pause` + `POST /api/v1/graph/resume` + 重跑动作组合测试完成。
- [ ] 失败分桶结果不得包含对话正文；只输出计数和错误前缀（hash 后）即可。

### US-004: 遇到非 recoverable 失败时的保护闭环
**描述：** 作为系统稳定性负责人，我要在重试无法解决时快速识别不可恢复项，避免高并发放大无效调用。

**Acceptance Criteria：**
- [ ] 在重试策略中定义“不重试清单”（例如模型鉴权敏感报错等）并与 `graph` 统计可见。
- [ ] 当 `last_error` 长期持久且 `failed` 无下降时，能按时间窗口触发并发降级（比如 5→3 或 3→1）。
- [ ] 任何一次降级都能通过 `GET /api/v1/graph/config?format=json` 与 `GET /api/v1/graph/status?format=json` 被证实。
- [ ] 不影响 `graph_source_records` 已完成数据与实体图谱结果。

## 4) Functional Requirements

- FR-1: 系统必须保留现有图谱 API，不新增全局删除/清空路径。
- FR-2: 系统必须支持 `POST /api/v1/graph/config` 的并发参数设置并持久化到图谱元数据。
- FR-3: 系统必须支持 `POST /api/v1/graph/rebuild` 的增量模式（`reset=false`）用于失败源重入队。
- FR-4: 系统必须在重跑前后通过 `/api/v1/graph/status?format=json` 提供 `pending/processing/processed/failed/source_count/workers/enqueue_workers/last_error/processing_rate_per_minute` 可观测字段。
- FR-5: 运行时必须能用只读 SQL 快照失败分布，不要求导出或展示消息正文。
- FR-6: 当运行策略失控（failed、last_error 上升）时，必须有明文操作手册与命令说明可回滚到 3 并发或 1 并发。
- FR-7: 任何并发调参必须在 `progress.md` 记录下时间戳和参数变化。

## 5) Non-Goals（超出范围）

- 不包含新的图谱模型推理算法。
- 不包含对历史图谱事实（entity/relation/event/fact）的一次性全量重建（除非运维手工另行执行 `reset=true` 的现有操作）。
- 不包含把敏感错误自动归类到外部告警系统。
- 不包含 UI 仪表盘开发。

## 6) Design Considerations

- API 路径均采用现有 REST 路径，不新增新 UI。
- 避免在恢复时把大量失败源一次性并发放大到超阈值；先“recoverable-only 重跑 + 并发观察”再决定是否扩大。
- 错误码与错误前缀在日志与报告中统一标准化展示，便于复盘。

## 7) Technical Considerations

- 依赖现有图谱状态接口：
  - `GET /api/v1/graph/status`
  - `GET /api/v1/graph/config`
  - `POST /api/v1/graph/config`
  - `POST /api/v1/graph/rebuild`
  - `POST /api/v1/graph/pause`
  - `POST /api/v1/graph/resume`
- 依赖现有 `temporalgraph.Store` 与 `Status` 字段；并发上限在 `maxGraphWorkers=12` 之内，`5/5` 可用。
- 仅允许只读 `sqlite` 查询来做失败分桶。
- 敏感上下文（消息正文、聊天摘要）不进入日志。

## 8) 成功指标

- 触发一次 `rebuild?reset=false` 后 15 分钟内：`failed` 持续下降并保持低波动。
- 5 并发时 `processing_rate_per_minute` 相对当前基线至少不下降 20%。
- 若并发拉满后仍出现持续错误，5 分钟内可回落并恢复稳定处理。
- 全程输出仅包含 `status`、计数、错误前缀和时间戳，未出现隐私字段。

## 9) Open Questions

- 失败分桶中哪些错误应被列为非 recoverable（不可自动重试）？
- 是否允许将并发固定在 5；还是按“历史错误率”动态降级为 3、2。
- 是否需要补一条“重试会话级幂等”验收（避免重复调用导致重复入队）？
- 是否要把本次恢复流程固化成脚本（例如 `scripts/graph/recover-failed-sources.sh`）并加入 `scripts/check-root-harness.mjs` 的可执行步骤。
