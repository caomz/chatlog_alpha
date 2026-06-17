# PRD: Temporal Graph HA Key Pool & Adaptive Model Routing

## 1. Introduction / Overview

Temporal Graph currently depends on remote Chat model calls to extract entities, facts, events, relations, and timeline signals from local WeChat-derived source records. Recent runtime incidents showed that the graph worker can enter a high-failure state when the service starts with the wrong config, the runtime process cannot see all `MINIMAX_API_KEYS`, or provider/model settings drift to an unconfigured Chat provider.

This feature builds a productized high-availability path for Temporal Graph extraction: five MiniMax API keys are used efficiently through observable key-pool scheduling, graph workers adapt to runtime health, recoverable failures are requeued safely, non-recoverable failures are preserved in explicit buckets, and model selection balances accuracy and throughput through a benchmark-gated `m2.1` fast path instead of an unverified global downgrade.

## 2. Goals

- Ensure Temporal Graph extraction remains available when runtime config drifts, keys are missing from the service process, or recoverable provider errors occur.
- Use 5 configured MiniMax API keys efficiently without exposing secret values in logs, HTTP responses, status files, or PRD evidence.
- Keep graph extraction `workers` aligned with safe key-pool capacity during stable periods, and reduce concurrency automatically when timeouts, rate limits, or upstream errors increase.
- Preserve extraction accuracy by keeping `MiniMax-M2.7` as the default bulk path until `m2.1` passes a benchmark gate on JSON validity and graph quality.
- Classify graph source failures into recoverable and non-recoverable buckets before requeueing, so sensitive/content-policy failures are not blindly retried.
- Provide a restartable operator workflow through HTTP status, readonly SQLite bucket checks, focused tests, and a guard command.

## 3. User Stories

### US-001: Runtime Chat readiness is observable and self-healable
**Description:** 作为本地运维执行者，我想要系统明确显示 Temporal Graph 所依赖的 Chat provider、model、API key 可用性和启动配置来源，以便在配置漂移时能自动恢复到可处理状态。

**Acceptance Criteria:**
- [ ] 启动服务后，`GET /api/v1/semantic/config?format=json` 返回 `chat_provider=mmx`、目标 `chat_model`、`has_api_key=true`。
- [ ] 新增或扩展的 runtime status 返回 `configured_key_count=5`，但响应中不包含任何以 `sk-` 开头的真实 key。
- [ ] 当服务以错误配置启动并导致 `chat model is not configured` 时，运行 HA guard 后 60 秒内再次查询 semantic config，返回 `chat_provider=mmx`、`has_api_key=true`、`configured_key_count=5`。
- [ ] HA guard 的 dry-run 输出只包含配置状态、key 数量、tmux/session/process 可见性和计划动作，不打印真实 API key。
- [ ] `bash -n scripts/chatlog-ha-guard.sh` 通过。

### US-002: MiniMax 5-key pool status can be inspected safely
**Description:** 作为系统调优者，我想要看到 MiniMax key pool 的可用 key 数、忙闲状态和错误桶，以便判断并发是否真的吃到了 5 个 key，而不是只靠环境变量猜测。

**Acceptance Criteria:**
- [ ] `GET /api/v1/semantic/mmx/status?format=json` 或等价状态接口返回 `configured_key_count=5`、`busy_key_count`、`idle_key_count`、`leased_request_count`、`retry_count`、`last_error_bucket`。
- [ ] 状态接口对 key 只返回稳定脱敏 label，例如 `key_1`、`key_2`，不返回完整 key、key 前缀或可还原片段。
- [ ] 使用 5 个逗号分隔的 `MINIMAX_API_KEYS` 启动服务后，状态接口稳定显示 `configured_key_count=5`。
- [ ] 当其中一个 key 出现可识别的鉴权错误时，状态接口显示该 key label 进入隔离或失败状态，后续请求不会继续优先使用该 key。
- [ ] `go test ./internal/chatlog/semantic` 覆盖 key count、脱敏输出、错误 key 隔离和敏感错误不切 key。

### US-003: Graph workers adapt to key-pool health
**Description:** 作为 Temporal Graph 使用者，我想要系统在稳定时自动使用 5 并发，在 rate-limit、timeout 或上游延迟上升时自动降级，以便兼顾处理效率和失败率。

**Acceptance Criteria:**
- [ ] 当 `configured_key_count=5` 且最近观察窗口没有 rate-limit/timeout 激增时，`GET /api/v1/graph/status?format=json` 显示 `workers=5`。
- [ ] 当最近观察窗口内 `rate_limited`、`network_timeout` 或 `before_request_timeout` 数量超过阈值时，系统在下一次调度窗口内将 `workers` 降到小于 5，并能通过 `GET /api/v1/graph/config?format=json` 读回。
- [ ] 降级后，如果连续稳定窗口内 `failed` 不增加、`last_error` 为空或只有非重试桶，系统逐步恢复到不超过 `configured_key_count` 的 worker 数。
- [ ] `workers` 永远不超过 `configured_key_count` 和现有 `maxGraphWorkers` 上限。
- [ ] `go test ./internal/chatlog/temporalgraph` 覆盖稳定升并发、错误降并发、恢复升并发和上限保护。

### US-004: Failed sources are bucketed before any requeue
**Description:** 作为数据安全负责人，我想要系统先按错误类型统计 failed sources，再只重试可恢复错误，以便避免把 sensitive 或无效请求失败反复打到模型 API。

**Acceptance Criteria:**
- [ ] readonly SQLite bucket 查询或等价 HTTP 状态能统计 `config_error`、`network_timeout`、`before_request_timeout`、`rate_limited`、`auth_error`、`json_decode_error`、`empty_graph`、`sensitive_input_1026`、`sensitive_output_1027`、`non_retryable_request`。
- [ ] `POST /api/v1/graph/resume?format=json` 只 requeue recoverable buckets：`config_error`、`network_timeout`、`before_request_timeout`、`rate_limited`。
- [ ] `sensitive_input_1026` 和 `sensitive_output_1027` 在 resume 后仍保持 failed，不进入 pending。
- [ ] requeue 前后状态快照包含 `failed`、`pending`、`processed`、`processing`、`last_error`，不包含私聊正文、source payload 或 prompt 内容。
- [ ] `go test ./internal/chatlog/temporalgraph` 覆盖 recoverable 与 non-recoverable bucket 的 requeue 行为。

### US-005: Model routing keeps M2.7 as baseline and gates m2.1 by benchmark
**Description:** 作为效果评估者，我想要用脱敏 fixture 对 `m2.1` 和 `MiniMax-M2.7` 做同 prompt 对比，以便只有在准确性达标时才把 `m2.1` 用作 fast path。

**Acceptance Criteria:**
- [ ] 提供一个不含真实聊天正文的 benchmark fixture，覆盖短句、省略语、时间表达、多人对话、业务实体、关系方向和空信息样本。
- [ ] benchmark 输出至少包含 `json_valid_rate`、`non_empty_graph_rate`、`entity_count_delta`、`fact_count_delta`、`event_count_delta`、`relation_count_delta`、`decode_retry_rate`、`avg_latency_ms`。
- [ ] 如果 `m2.1` 的 `json_valid_rate` 低于 `MiniMax-M2.7` 超过 2 个百分点，或 `non_empty_graph_rate` 低于 `MiniMax-M2.7` 超过 5 个百分点，系统不得把 `m2.1` 设置为默认 fast path。
- [ ] 如果 `m2.1` 通过 benchmark，系统只将它用于低风险 bulk extraction；`json_decode_error`、低质量输出和关键来源仍升级到 `MiniMax-M2.7` 或配置的质量兜底模型。
- [ ] benchmark 命令默认 dry-run 或 fixture-only，不调用真实私聊 source 内容。

### US-006: HA guard becomes a supported operator workflow
**Description:** 作为长期运行任务维护者，我想要一个可重复执行的 guard 命令检查 health、semantic config、key count、graph status 和失败桶，以便 graph worker 可以在 tmux/launchd 场景下自动恢复。

**Acceptance Criteria:**
- [ ] `scripts/chatlog-ha-guard.sh --dry-run` 返回当前 health、semantic provider/model、key count、workers、failed bucket 摘要和计划动作。
- [ ] `scripts/chatlog-ha-guard.sh` 在服务健康、config 正确、key count 为 5、workers 正确时重复运行不会改变状态。
- [ ] 当 guard 发现服务未使用 `serve --config-dir .cache/daily-report-config` 或 process key count 不是 5 时，会执行明确恢复动作，并在输出中记录动作结果。
- [ ] `scripts/chatlog-ha-guard.sh --loop 60` 能以 60 秒间隔运行，并在每轮输出时间戳、状态摘要和是否采取动作。
- [ ] guard 的所有输出不包含真实 API key、私聊正文、source payload、prompt 内容。

### US-007: End-to-end HA validation proves throughput without hiding failures
**Description:** 作为项目负责人，我想要一条完整验收路径证明 5-key HA 方案既能处理队列，又不会把失败和敏感错误掩盖掉。

**Acceptance Criteria:**
- [ ] 在服务运行时执行 `curl -sS http://127.0.0.1:5030/health`，返回 `{"status":"ok"}`。
- [ ] 执行 semantic status、MMX key-pool status、graph status 后，能看到 `chat_provider=mmx`、`has_api_key=true`、`configured_key_count=5`、`workers<=5`。
- [ ] 运行一次 recoverable resume 后，30 秒内 `pending` 或 `processed` 至少一个发生预期变化，`sensitive_input_1026` / `sensitive_output_1027` 仍保持 non-requeued。
- [ ] 运行 10 分钟观察窗口后，`processed` 增加，`failed` 不因 config drift 大规模增长，`last_error` 为空或属于已知 bucket。
- [ ] 执行 `go test ./internal/chatlog/semantic ./internal/chatlog/temporalgraph` 和 `./init.sh` 均通过，或在 PR/进度记录中明确列出失败命令、exit status、关键错误行和下一步修复。

## 4. Functional Requirements

- FR-1: 系统必须提供 runtime 可观测字段，显示 semantic Chat provider、model、API key 是否可用、configured key count 和配置来源。
- FR-2: 系统必须提供 MiniMax key-pool 状态，至少包含 key 数、忙闲数量、lease 请求计数、重试计数和错误桶，不得暴露任何真实 API key。
- FR-3: Temporal Graph worker 数必须受 `configured_key_count`、`maxGraphWorkers` 和自适应健康窗口共同约束。
- FR-4: 系统必须将 recoverable failures 与 non-recoverable failures 分桶，并在 resume/requeue 时只处理 recoverable buckets。
- FR-5: `input new_sensitive (1026)` 与 `output new_sensitive (1027)` 必须保持 non-retryable，不得通过轮换 key 或升级模型绕过。
- FR-6: 系统必须支持 `MiniMax-M2.7` 作为默认 bulk extraction baseline。
- FR-7: 系统只有在 benchmark 指标达标后，才允许把 `m2.1` 配置为低风险 fast path。
- FR-8: HA guard 必须支持 one-shot、dry-run 和 loop 模式，并能在不打印秘密的情况下恢复 config drift、key env drift 和 recoverable graph failures。
- FR-9: 所有运行态验证必须优先使用 `format=json` HTTP 响应、只读 SQLite 计数、文件元数据和测试结果，不打印私聊正文或 prompt 内容。
- FR-10: 实施完成后必须更新 `feature_list.json`、`progress.md` 和 `session-handoff.md`，记录验证命令、未验证项、风险和下一步。

## 5. Non-Goals

- 不做分布式队列、云端 worker 集群或跨机器 HA。
- 不自动购买、轮换或管理 MiniMax 账户级 quota。
- 不把 sensitive 1026/1027 失败通过换 key、换模型或改写 prompt 自动绕过。
- 不默认读取、打印或导出真实聊天正文作为 benchmark 样本。
- 不在本 PRD 中实现完整前端 UI 面板；HTTP/CLI/脚本可观测性优先。
- 不把 `m2.1` 未经 benchmark 直接设为全量默认模型。
- 不执行 `reset=true` 的全量图谱重建，除非用户后续明确授权。

## 6. Design Considerations

- 状态输出面向本地运维和 AI agent 验证，优先选择稳定 JSON 字段，避免依赖人眼解析日志。
- 如果未来增加 UI，应该展示 key pool 的计数和状态 label，不展示 key 前缀、尾号或可还原片段。
- Guard 输出应适合复制进 `progress.md`：时间戳、health、provider/model、key_count、workers、failed bucket、action result。
- 错误桶命名要短且稳定，便于后续做 Obsidian 复盘、内容创作或 Ralph validator 检查。

## 7. Technical Considerations

- 主要影响子系统：
  - `internal/chatlog/semantic/`
  - `internal/chatlog/temporalgraph/`
  - `internal/chatlog/http/`
  - `cmd/chatlog/`
  - `scripts/chatlog-ha-guard.sh`
- 已有基础：
  - MiniMax key pool 已支持从 `MINIMAX_API_KEYS` 读取多个 key。
  - Temporal Graph 已有 `workers` / `enqueue_workers` 配置和 `graph/resume`。
  - 现有错误策略已将 MiniMax 1026/1027 标记为 non-retryable。
  - 现有 `scripts/chatlog-ha-guard.sh` 已覆盖部分 health/config drift 恢复。
- 推荐新增或扩展接口：
  - `GET /api/v1/semantic/mmx/status?format=json`
  - `GET /api/v1/graph/failure-buckets?format=json`
  - 或在现有 semantic/graph status 响应中增加等价字段。
- 测试优先级：
  - 语义层：key count、lease、脱敏、retry/switch 策略。
  - 图谱层：failure bucket、resume 策略、自适应 worker。
  - 脚本层：`bash -n`、`--dry-run`、幂等执行、不泄密。

## 8. Success Metrics

- 运行进程能稳定报告 `configured_key_count=5`，且日志和 HTTP 响应中零出现真实 `sk-` key。
- 在稳定窗口内，Temporal Graph `workers` 自动保持在 `5` 或不超过 key count 的安全值。
- 因 `chat model is not configured` 导致的大规模 failed source 能在 guard/resume 后回到 pending 或 processed，而不是继续增长。
- 10 分钟观察窗口内，`processed` 单调增加，`failed` 不因 config drift 或 timeout 雪崩式增长。
- Non-retryable buckets 保持可见，不被 resume 静默清空。
- `m2.1` 是否进入 fast path 有 benchmark 证据，而不是凭模型名或主观速度判断。

## 9. Verification Plan

```bash
bash -n scripts/chatlog-ha-guard.sh
scripts/chatlog-ha-guard.sh --dry-run

curl -sS --max-time 8 'http://127.0.0.1:5030/health'
curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/semantic/config?format=json'
curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/semantic/mmx/status?format=json'
curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/graph/status?format=json'
curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/graph/failure-buckets?format=json'

go test ./internal/chatlog/semantic ./internal/chatlog/temporalgraph
./init.sh
```

Readonly SQLite checks may be used to validate failed buckets, but must only output counts and normalized error buckets, never source content or prompt payload.

## 10. Open Questions

- `m2.1` benchmark 的 fixture 数量是否定为 100、300，还是按 buckets 分层抽样后固定每类最少样本数？
- 自适应 worker 的初始阈值应使用固定规则，还是从最近 10 分钟 processed/failed/timeout 比例动态计算？
- 是否需要为 HA guard 增加 `launchd` plist 模板，还是当前先以 tmux loop 作为长期保活方式？
- `json_decode_error` 是否应保持 failed，还是进入一个单独的 quality retry bucket，由更强模型或更严格 parser 处理？
