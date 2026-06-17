# PRD: Temporal Graph 个人知识沉淀摘要（Graph Knowledge Digest）

## Introduction

chatlog_alpha 的 temporal graph 已经在持续把本地微信聊天记录抽取成 entities / facts / events / relations / timeline（当前 source_count≈17355，processed≈5200+，worker 正在持续处理）。但这些结构化知识目前只能通过零散的 `GET /api/v1/graph/query`、`/timeline`、`/qa` 手工查询，没有一条「定期产出 → 本地沉淀」的固定通道。

本功能为用户（传统电信系统运维工程师）提供一个**定期生成的 Obsidian 友好 Markdown 知识摘要**：按时间窗口聚合图谱中的核心主题、关键实体、事件时间线和新增事实，输出到本地 `reports/` 目录，供个人复盘、故障/事项追溯和知识沉淀。摘要内容保持角色中立，不内置任何特定职业视角的 prompt。产出物本地可看，**永不提交、永不入库**。

第一版是最小可行版本（MVP）：默认纯聚合（不调用模型、不消耗 quota），模型综述作为显式可选项。

## Goals

- 一条命令 / 一次 HTTP 调用，即可对指定时间窗口（默认最近 7 天）生成图谱知识摘要 Markdown 文件。
- 摘要结构遵循用户个人提示词中的知识沉淀格式（标题、一句话结论、核心观点、时间线、可复用沉淀、后续选题）。
- 默认路径零模型调用、零 quota 消耗；模型综述必须显式开启。
- 产出文件落在 `reports/`（已在不可提交清单中），验证只看路径、大小、计数、章节标题，不在终端打印聊天衍生正文。

## User Stories

### US-001: temporalgraph 包提供 Digest 聚合能力
**描述：** 作为开发者，我需要 temporal graph manager 提供一个按时间窗口聚合的 `Digest` 方法，以便上层 HTTP/CLI 可以拿到结构化摘要数据而不是各自拼查询。

**Acceptance Criteria：**
- [ ] `internal/chatlog/temporalgraph` 新增 `Digest(start, end time.Time, opts DigestOptions) (DigestResult, error)`（命名可按包内风格微调，但必须是 manager 上的公开方法）
- [ ] `DigestResult` 至少包含：时间窗口、top 实体列表（名称+**窗口内出现次数**，上限可配，默认 20）、窗口内事件时间线条目（时间+标题，上限默认 50）、新增 facts/relations 计数、参与会话/source 计数
- [ ] 实体窗口内计数通过解析窗口内 `graph_events` 的 `actors_json`/`targets_json` 在 Go 侧统计（内部扫描上限 2000，暴露 truncated 标记）；不修改 `ApplyExtraction`、不依赖 `graph_evidence`（该表无 entity 行，已核实）
- [ ] 聚合只读 store 查询，不触发任何 Chat provider 调用，不修改队列状态（`pending`/`failed` 计数在调用前后一致）
- [ ] 窗口内无数据时返回空结果且 error 为 nil，不 panic
- [ ] 新增 `go test ./internal/chatlog/temporalgraph` 用例覆盖：正常窗口、空窗口、limit 截断三种情况，全部通过

### US-002: HTTP 端点生成摘要文件并返回元数据
**描述：** 作为用户，我想通过本地 HTTP 服务一次调用生成摘要文件，以便在服务常驻（tmux `chatlog-alpha`）的情况下随时产出，不需要另起进程读图谱 SQLite。

**Acceptance Criteria：**
- [ ] 新增 `POST /api/v1/graph/digest`，注册在 `internal/chatlog/http/route.go` 的 graph 路由组中
- [ ] 支持参数：`days`（默认 7）或显式 `start`/`end`、`format=json`、`summary`（默认 false）
- [ ] 调用成功后在**服务进程工作目录（CWD）**下的 `reports/` 生成 `graph-digest-<start>_<end>.md`（日期用 `YYYY-MM-DD`）；有意不复用 `resolveDailyReportOutputDir` 的 workDir 沙箱语义（digest 要落 repo 的 `reports` symlink 进知识库），代码注释说明分叉原因
- [ ] 目标目录不可写/写入失败时返回 HTTP 500 与错误类别，绝不静默返回 200（cron 场景的静默故障防护）
- [ ] `format=json` 响应只含元数据：`path`、`size_bytes`、`window_start`、`window_end`、`entity_count`、`event_count`、`fact_count`、`relation_count`、`summary_used`(bool)，**不含**任何摘要正文或聊天衍生文本
- [ ] 参数非法（如 `days<=0`、`start>end`）返回 400 和明确错误信息，且不生成文件
- [ ] `curl -s -X POST '127.0.0.1:5030/api/v1/graph/digest?days=7&format=json'` 返回 200，响应字段齐全，`ls -la` 能看到对应文件且大小 > 0

### US-003: 摘要 Markdown 采用 Obsidian 友好的知识沉淀结构
**描述：** 作为知识管理实践者，我想让摘要文件直接符合我的个人沉淀格式，以便拖进 Obsidian vault 就能用，不需要二次整理。

**Acceptance Criteria：**
- [ ] 生成的 Markdown 按固定章节顺序包含以下 H2 标题：`# 标题`（H1，含窗口日期）、`## 一句话结论`、`## 核心主题`、`## 关键实体`、`## 事件时间线`、`## 新增事实与关系`、`## 可复用沉淀`、`## 后续跟进`
- [ ] 默认（`summary=false`）模式下：`一句话结论` 为基于计数的模板句（如「本窗口共 N 个实体、M 条事件…」），`核心主题`/`可复用沉淀`/`后续跟进` 章节存在但标注 `（需开启 summary 生成）`；其余章节由聚合数据填充
- [ ] 文件头部含 YAML frontmatter：`date`、`window_start`、`window_end`、`source: chatlog temporal graph`、`tags: [chatlog, graph-digest]`
- [ ] 验证方式：`grep -c '^## ' reports/graph-digest-*.md` 返回 7，`head -8` 显示 frontmatter 字段齐全 —— 不在终端打印章节正文内容
- [ ] `go test ./internal/chatlog/temporalgraph`（或渲染所在包）含渲染单测：用合成数据断言章节齐全与 frontmatter 字段，不依赖真实聊天数据

### US-004: 可选模型综述（显式开启，quota 受控）
**描述：** 作为用户，我想在需要时让 Chat provider（当前 `mmx` / `MiniMax-M3`）对窗口内的图谱数据写一段综述，以便 `一句话结论`、`核心主题`、`后续跟进` 有真正的洞察，而不只是计数模板。综述 prompt 保持角色中立（聚焦事项归纳与待跟进项），不内置任何职业身份设定。

**Acceptance Criteria：**
- [ ] summary 逻辑与全部 mock 单测位于 `internal/chatlog/temporalgraph`（Chat provider 接口注入），http handler 只做参数透传（http 包现无测试基建，不为透传层新建）
- [ ] 仅当请求带 `summary=true` 时才调用 Chat provider，且整个 digest 最多发起 1 次模型调用
- [ ] 发送给模型的 payload 复用 `internal/chatlog/temporalgraph/helpers.go` 既有脱敏路径（不含 raw `talker_id`/`sender_id`，内容截断）
- [ ] Chat provider 未配置或调用失败时：digest 仍成功生成（降级为 `summary=false` 的模板内容），JSON 响应中 `summary_used=false` 并附 `summary_error` 字段（只含错误类别，不含 prompt 内容）
- [ ] 该验收默认 `未验证`（quota/隐私敏感）：实现后仅以单测 mock provider 验证降级逻辑；真实模型调用须由用户显式发起一次并只核对 `summary_used=true` 与文件 size 变化

### US-005: CLI 入口 `chatlog report graph`
**描述：** 作为用户，我想用一条本地命令生成摘要，以便配合 cron/launchd 做定期沉淀，不必手写 curl。

**Acceptance Criteria：**
- [ ] `cmd/chatlog/cmd_report.go` 下新增子命令 `chatlog report graph`，支持 `--days`（默认 7）、`--summary`（默认 false）、`--base-url`（默认 `http://127.0.0.1:5030`）
- [ ] 命令通过 US-002 的 HTTP 端点工作；服务未运行时打印明确错误（含端点地址与「先启动 chatlog serve」提示），exit code 非 0
- [ ] 成功时 stdout 只打印：生成文件路径、窗口、各 count、`summary_used` —— 不打印摘要正文
- [ ] `go run . report graph --help` 显示上述 flags 与默认值
- [ ] 服务端返回 500 时 CLI 打印错误类别并以非零 exit code 退出
- [ ] 新增 `docs/graph-digest.md`：命令用法、参数、同窗口幂等覆盖策略、quota/隐私边界四节（风格参照 `docs/daily-report.md`）
- [ ] `go build ./...` 与 `node scripts/check-root-harness.mjs` 通过

### US-006: 真实闭环集成验证
**描述：** 作为用户，我要确认从命令到本地文件的完整链路在真实运行的服务上成立，并且产出物不会被提交。

**Acceptance Criteria：**
- [ ] 在 `chatlog-alpha` 服务运行状态下执行 `go run . report graph --days 7`，exit code 0
- [ ] `reports/` 出现当次 `graph-digest-*.md`，`stat` 显示生成时间为当次执行、大小 > 0
- [ ] `grep -c '^## '` 该文件返回 7；JSON/stdout 各 count 与 `GET /api/v1/graph/status` 的总量级一致（不要求精确相等，要求非负且 ≤ 总量）
- [ ] `git status --porcelain` 输出中不出现任何 `graph-digest-*.md` 条目（注意：`reports` 是指向 `/Volumes/WorkSSD/Dev/openclaw_mz/knowledge/raw/微信每日聊天记录` 的 symlink，`?? reports` 一行是启动前已有状态，不属于本功能，也绝不能被提交）
- [ ] 再次以相同窗口执行，文件被幂等覆盖或带序号区分（行为二选一并在 docs 中写明），不产生报错
- [ ] 执行前后 `graph/status` 的 `failed` 计数不增加（digest 不干扰处理队列）

## Functional Requirements

- FR-1: 系统必须提供 manager 级 `Digest` 聚合：按 [start,end) 窗口返回 top 实体、事件时间线、facts/relations 计数，只读、不调模型。
- FR-2: 系统必须提供 `POST /api/v1/graph/digest`，参数 `days|start/end`、`summary`、`format=json`，生成 `reports/graph-digest-<start>_<end>.md` 并返回纯元数据 JSON。
- FR-3: Markdown 产出必须含 YAML frontmatter 和固定 7 个 H2 章节（见 US-003），结构对齐用户个人提示词的知识沉淀格式。
- FR-4: `summary=true` 时系统最多调用 Chat provider 1 次，payload 走既有脱敏路径；失败必须降级而不是让 digest 失败。
- FR-5: 系统必须提供 `chatlog report graph` CLI 子命令封装 FR-2，stdout 只输出元数据。
- FR-6: digest 全链路不得修改图谱处理队列状态，不得在 API 响应、stdout、日志中输出聊天衍生正文。
- FR-7: 产出文件必须落在 `reports/`，且保持被 git ignore；任何验收都以路径/大小/时间戳/计数/章节标题为准。

## Non-Goals

- 不做调度本身（cron/launchd/定时器由用户自行配置；本功能只保证命令可被定时调用且幂等）。
- 不做 Obsidian vault 自动写入/同步（用户手动拖入或后续单独立项）。
- 不做面向发布的内容生产，不在 prompt 或模板中内置任何职业角色设定（如博主/创作者视角）。
- 不做 HTML 渲染、图表、`graph/visualize` 集成。
- 不修改 graph 抽取/队列/worker 逻辑（当前 active feature `temporal-graph-reprocess-failed-2026-06-04` 的处理流程保持不动）。
- 不新增模型 provider 或改动 MiniMax key 池逻辑。

## Technical Considerations

- 图谱 SQLite 由 serve 进程持有，digest 必须经 HTTP 走进程内 manager，避免 CLI 直接并发打开 DB。
- 当前 worker 正在大批量处理（pending≈12000），digest 的只读查询应带 limit，避免全表扫描拖慢 worker；US-001 的默认上限即为此设计。
- Chat provider 现为 `mmx` / `MiniMax-M3`（`.cache/daily-report-config/chatlog-server.json`），`summary=true` 走 `internal/chatlog/semantic` 既有 client，沿用 1026/1027 非重试与 key 池逻辑，不另起炉灶。
- 渲染风格可参考 `internal/chatlog/dailyreport/renderer.go` 的现有模式，但 digest 渲染放在 temporalgraph 侧或独立小包，不要把 dailyreport 拖进依赖。
- 文件命名冲突策略（覆盖 vs 序号）在实现时定一种，写入 `docs/`（建议新增 `docs/graph-digest.md` 简述用法与隐私边界）。

## Success Metrics

- 从零到一份当周知识摘要 ≤ 1 条命令、≤ 30 秒（不含 summary 模型调用）。
- 默认路径模型调用次数 = 0；`summary=true` 时 = 1。
- 摘要文件无需手工改格式即可放入 Obsidian（frontmatter + 固定章节）。
- `reports/` 产物零次进入 git 暂存区。

## Open Questions

- top 实体的排序口径：按出现次数还是按关联事件数？MVP 先用出现次数，后续可加参数。
- 事件时间线超过 50 条时是截断还是分日聚合？MVP 截断并在文末标注总数。

（已确认：summary prompt 不内置任何职业角色设定，保持中立的事项归纳视角 —— 用户为传统电信系统运维工程师，2026-06-10 确认。）
