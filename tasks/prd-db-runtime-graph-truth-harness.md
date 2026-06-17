# PRD: DB Runtime, Temporal Graph Truth, and Harness Recovery

## Introduction

当前 `chatlog_alpha` 的 HTTP 服务在线，但 WeChat DB 查询链路出现系统性失败：`/api/v1/db` 可以列出 19 个数据库，核心库 `/api/v1/db/tables` 却全部返回 `file is not a database`。同时 `all_keys.json` 明显早于当前 `session/contact/message` 源 DB 与 WAL，`~/.chatlog/wcdb_cache/*.db` 虽有 SQLite header，但 `sqlite_master` 查询失败，说明解密产物不可读。

本功能目标是把 DB runtime 从“看起来可用”恢复到“核心库可查询”，同时强化 temporal graph 的健康判定，避免把 `health ok`、`progress_pct=100`、`all_keys.json exists`、`wcdb_cache exists` 误报成完成态。由于后续可能交给能力较弱的 MiniMax 模型执行，PRD 必须拆成小 story，每个 story 只改变一个变量，并写清楚命令级验收标准。

## Goals

- 恢复 HTTP / 前端 DB 面板核心链路，使 `session/session.db`、`contact/contact.db`、`message/message_0.db` 至少能通过 `/api/v1/db/tables` 查询。
- 防止坏解密产物进入 `wcdb_cache` 并被后续请求继续命中。
- 将 “有 key / 有 cache / 有 DB 文件” 与 “数据库可查询” 明确区分。
- 保证 temporal graph 健康判断可验证、可恢复、不可误报。
- 把本次排障方法沉淀到 `skills/chatlog-http-cli`，形成后续 agent 可复用的 runtime truth chain。
- 保持隐私边界：不打印聊天正文、真实 API key、真实 data key。

## User Stories

### US-001: 建立只读诊断基线
**描述：** 作为维护者，我需要先得到不泄密的 DB / graph 当前事实，以便后续每一步修复都能对比前后变化。

**Acceptance Criteria:**
- [ ] 在 `/Volumes/WorkSSD/Dev/chatlog_alpha` 下确认 `pwd` 为项目根目录。
- [ ] `curl -sS --max-time 8 http://127.0.0.1:5030/health` 返回 `{"status":"ok"}`。
- [ ] `curl -sS --max-time 15 http://127.0.0.1:5030/api/v1/db` 能统计出 DB 总数，不打印聊天内容。
- [ ] 对 `session/session.db`、`contact/contact.db`、`message/message_0.db` 分别调用 `/api/v1/db/tables`，记录 HTTP status 与错误摘要。
- [ ] 检查 `all_keys.json` mtime 与 `session.db`、`session.db-wal`、`contact.db`、`message_0.db` mtime，只记录路径、mtime、size，不打印 key。
- [ ] 检查 `~/.chatlog/wcdb_cache/*.db` 数量、mtime 范围、`sqlite_master` 可读失败 bucket，不打印 DB 内容。
- [ ] 写入 `progress.md` 的 `Verification Evidence`，明确当前是否仍是 `file is not a database`。

### US-002: 后端拒绝不可读的解密产物
**描述：** 作为后端开发者，我需要确保 `ensureDecrypted()` 不会把不可读的伪 SQLite cache 当作成功产物，以便错误能停在解密阶段而不是污染 HTTP 查询链路。

**Acceptance Criteria:**
- [ ] `internal/wechatdb/wcdbapi/client.go` 中任何 decrypt 完成后，在 `os.Rename(tmpPath, outPath)` 前必须调用 `isReadableSQLite(tmpPath)`。
- [ ] 如果 `isReadableSQLite(tmpPath)` 返回错误或 false，系统删除 `tmpPath`，不写入 `outPath`，不更新 `c.cache`。
- [ ] 当 cache 命中但 `sqlite_master` 不可读时，系统删除该 cache 文件并重新尝试解密；如果重新解密仍不可读，返回可行动错误。
- [ ] 返回错误信息必须包含可行动提示，例如 `decrypted database is unreadable; all_keys.json may be stale or invalid; rescan WeChat keys and clear wcdb cache after backup`，但不得包含真实 key。
- [ ] `go test ./internal/wechatdb/...` 通过。
- [ ] 新增 focused test 覆盖“文件 header 是 `SQLite format 3`，但 `sqlite_master` 查询返回 `file is not a database`”的场景。

### US-003: DB 列表不再把“有 key”误报成“可查询”
**描述：** 作为前端用户，我需要 DB 面板看到明确的可用性状态，而不是列表能显示、点开全失败。

**Acceptance Criteria:**
- [ ] `GetDBs` / `GetDecryptedDBs` 或对应 datasource 输出中，能够区分 `listed` 与 `queryable` 状态。
- [ ] 如果某 DB 只存在于 `all_keys.json` 但 `/db/tables` 不可读，HTTP 层不得把它静默标成可查询。
- [ ] `/api/v1/db` 的响应保留兼容字段，同时新增或提供可机读的不可用原因字段；原因中不得包含 key。
- [ ] 前端 DB 面板在核心 DB 不可用时显示明确错误：`数据库当前不可用，请重扫 key 或检查 wcdb cache`，不再只显示泛化 `加载失败`。
- [ ] 使用浏览器打开 `http://127.0.0.1:5030/`，进入 DB 面板；当后端返回不可用原因时，页面出现明确错误状态，无控制台未处理异常。
- [ ] `go test ./internal/wechatdb/...` 通过。

### US-004: 安全恢复 key 与坏 cache
**描述：** 作为维护者，我需要用可回滚方式刷新 stale key 和坏 cache，以便核心 DB 从不可读恢复为可查询。

**Acceptance Criteria:**
- [ ] 备份当前 `all_keys.json`，备份文件路径包含时间戳，记录 path / size / mtime，不打印 key。
- [ ] 记录当前 `~/.chatlog/wcdb_cache` 中坏 cache 文件数量；不批量删除，除非用户明确授权。
- [ ] 如果需要清理 cache，必须先说明目标路径、风险、回滚方式；执行时只能一次处理一个文件，或改为用户手动确认批量清理。
- [ ] 强制重扫 WeChat key 后生成新的 `all_keys.json`，记录新文件 path / size / mtime，不打印 key。
- [ ] 重启 `chatlog-alpha` 服务后，`/health` 返回 OK。
- [ ] 对 `session/session.db`、`contact/contact.db`、`message/message_0.db` 调用 `/api/v1/db/tables`，HTTP status 为 200，返回 tables 数量大于 0。
- [ ] 如果任一核心 DB 仍失败，记录 exact command、HTTP status、关键错误行和下一步，不声称已修复。

### US-005: 前端 DB 面板闭环验证
**描述：** 作为用户，我需要确认前端 DB 面板不再整体报错，以便后续可以继续使用本地数据查询能力。

**Acceptance Criteria:**
- [ ] 使用浏览器打开 `http://127.0.0.1:5030/`。
- [ ] 进入数据库调试 / DB 面板后，页面能列出 DB 分组。
- [ ] 点击或加载 `session/session.db`、`contact/contact.db`、`message/message_0.db` 时，页面显示 tables 列表，不出现 `file is not a database`。
- [ ] 如果某个非核心 DB 仍不可用，页面只标记该 DB 的错误，不阻断整个 DB 面板。
- [ ] 浏览器控制台无未捕获异常。
- [ ] 验证过程中不打印聊天表数据或消息正文。

### US-006: Temporal graph 健康状态真值链
**描述：** 作为维护者，我需要用 graph status、SQLite bucket 和 digest 输出共同判断图谱状态，以便不再把 `progress_pct=100` 当成健康完成态。

**Acceptance Criteria:**
- [ ] `curl -sS --max-time 12 'http://127.0.0.1:5030/api/v1/graph/status?format=json'` 可读。
- [ ] status 中 `last_error` 为 `null` 或空字符串。
- [ ] 使用 readonly SQLite 查询 `graph_source_records`，统计 `done`、`failed`、`pending`、`processing`，不打印 source content。
- [ ] failed bucket 明确包含 `chat_model_not_configured`、`auth_401`、`timeout`、`sensitive`、`json_decode_or_format` 或 `other` 分类；确认 `file is not a database` / `db_not_database` bucket 为 0。
- [ ] 不执行 `graph/resume`、`graph/rebuild`、`graph/pause`、`graph/config`，除非另一个 story 明确要求。
- [ ] `progress.md` 记录：`progress_pct=100` 只代表当前 pending 为 0，不代表 failed 为 0 或图谱完全健康。

### US-007: Graph digest non-summary 验证不扰动队列
**描述：** 作为维护者，我需要确认 `chatlog report graph --days 7` 能生成 digest，并且不会增加 graph failed count。

**Acceptance Criteria:**
- [ ] `go run . report graph --help` 显示 `--summary` 默认关闭。
- [ ] 运行前记录 HTTP status 中的 `failed` 数和 SQLite `failed` 数。
- [ ] 运行 `go run . report graph --days 7` 成功，默认 non-summary path 不触发模型摘要。
- [ ] 只验证输出文件 path、mtime、size、H2 section count，不打印 digest 正文。
- [ ] digest 文件包含 7 个 `## ` H2 section。
- [ ] 运行后 HTTP `failed` 数和 SQLite `failed` 数与运行前一致。
- [ ] 如果 failed count 增加，记录 exact command、前后计数和错误摘要，不声称 graph 验证通过。

### US-008: `skills/chatlog-http-cli` runtime truth 方法论写回
**描述：** 作为后续 agent，我需要在 repo-local skill 中看到明确排障路径，以便以后遇到同类问题时不会重复误判。

**Acceptance Criteria:**
- [ ] `skills/chatlog-http-cli/SKILL.md` 的 Verification Gate 明确说明 `/health` 和 `/ping` 只是 availability precheck，不是 DB / graph 完成态。
- [ ] 新增或更新 reference，写入 WeChat DB/key/cache truth chain：AppleDouble / stale key / bad cache 三分法。
- [ ] 新增或更新 reference，写入 temporal graph truth chain：HTTP status + SQLite failed bucket + digest non-summary path。
- [ ] `feedback-audit.md` 或等价 reference 列出 Known False Greens：`health ok`、`all_keys.json exists`、`wcdb_cache exists`、`progress_pct=100`、`processing=N`。
- [ ] `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs` 通过。
- [ ] 文档不得包含真实聊天正文、真实 API key、真实 data key。

### US-009: Root state 与完成态记录
**描述：** 作为下一轮维护者，我需要从 state files 直接恢复上下文，以便不用依赖聊天记录。

**Acceptance Criteria:**
- [ ] `feature_list.json` 记录本功能 id、scope、done criteria、evidence、status、next_step。
- [ ] `progress.md` 记录 Current State、What changed、Verification Evidence、Not Verified、Blockers/Risks、Next。
- [ ] `session-handoff.md` 记录下一会话可直接运行的诊断 / 验证命令。
- [ ] 所有未运行的检查写为 `未验证` 并说明原因。
- [ ] 不把 `reports/`、`reports.backup-*`、`.cache/`、`logs/`、`outputs/` 作为提交对象。
- [ ] 不执行 `git commit`、`git push`、`git reset`、`git rebase`、`git merge`。

### US-010: 端到端恢复验收
**描述：** 作为维护者，我需要一个最终闭环，证明 DB 链路、graph 验证、harness 文档和项目状态文件全部对齐。

**Acceptance Criteria:**
- [ ] `/api/v1/db/tables` 对 `session/session.db`、`contact/contact.db`、`message/message_0.db` 返回 200 且 tables 数量大于 0。
- [ ] 前端 DB 面板加载核心库 tables，不再整体显示 `file is not a database`。
- [ ] `/api/v1/graph/status?format=json` 可读，`last_error=null`。
- [ ] graph failed bucket 中 `file is not a database` / `db_not_database` 为 0。
- [ ] `go run . report graph --days 7` 成功，digest 文件 path/mtime/size/H2 count 已验证，运行前后 graph failed count 不变。
- [ ] `go test ./internal/wechatdb/... ./internal/chatlog/temporalgraph` 通过。
- [ ] `./init.sh` 通过。
- [ ] 如果 `./init.sh --full` 或 runtime 敏感检查未运行，最终报告明确写 `未验证` 和原因。
- [ ] 最终报告只输出：是否达标、差距在哪里、可写回 skill 的方法论、应删除或降级的规则。

## Functional Requirements

- FR-1: 系统必须在写入 WCDB 解密 cache 前验证 `sqlite_master` 可读性。
- FR-2: 系统必须在 cache 命中时继续执行 strict readable check。
- FR-3: 系统必须在解密产物不可读时返回可行动错误，不得把坏 cache 当成功。
- FR-4: 系统必须区分 DB 文件存在、key 存在、cache 存在、DB 可查询四种状态。
- FR-5: HTTP DB 列表必须能表达不可查询原因，且不得泄露 key。
- FR-6: 前端 DB 面板必须能显示局部 DB 错误，不因单个 DB 失败导致整体不可用。
- FR-7: graph 健康验证必须包含 HTTP status、readonly SQLite bucket、digest non-summary path 三部分。
- FR-8: graph failed rows 不得在未分桶前被批量 requeue。
- FR-9: repo-local skill 必须记录 `file is not a database` 三分法和 graph false-green 规则。
- FR-10: 所有验证证据必须避免打印聊天正文、真实 API key、真实 data key。

## Non-Goals

- 不重新设计 WeChat key 扫描算法。
- 不修改 MiniMax / MMX 模型调用策略。
- 不盲目 requeue temporal graph failed rows。
- 不清空或重建 temporal graph 数据库。
- 不批量删除文件或目录。
- 不提交、推送、reset、rebase、merge。
- 不生成或发布任何内容平台文案。
- 不把 graph failed count 清零作为本功能目标；本功能只要求 DB 问题不进入 graph failed bucket，并能正确分桶解释现有失败。

## Design Considerations

- 前端 DB 面板错误应简短、明确、可操作，例如：
  - `数据库当前不可用：解密 cache 不可读。请重扫 key 或按排障文档检查 wcdb cache。`
- 对核心库和非核心库应分层展示：核心库失败影响主要功能，非核心库失败只标记局部不可用。
- 不在 UI 中显示真实路径的敏感片段；必要时只显示 basename 或脱敏后的相对路径。

## Technical Considerations

- 关键代码区域：
  - `internal/wechatdb/wcdbapi/client.go`
  - `internal/wechatdb/datasource/wcdb/datasource.go`
  - `internal/chatlog/http/route.go`
  - `internal/chatlog/http/static/index.htm`
  - `skills/chatlog-http-cli/SKILL.md`
  - `skills/chatlog-http-cli/references/*.md`
- `isReadableSQLite(path)` 已存在，应作为统一 truth check。
- “SQLite header 存在但 `sqlite_master` 不可读”必须作为失败，不得被视为成功。
- 强制重扫 key 和清 cache 是运行态操作，必须先备份、再单步、再复验。
- `chatlog report graph --days 7` 默认 non-summary path 不应触发模型调用；`--summary` 属于 quota-sensitive，不纳入默认验收。
- 当前工作区已有 dirty files，实施时不得 revert 用户或其他 run 的改动。

## Success Metrics

- 核心 DB `/db/tables` 成功率从 0/3 提升到 3/3。
- `~/.chatlog/wcdb_cache` 中新生成的核心 cache 通过 `sqlite_master` 可读性验证。
- 前端 DB 面板不再因 `file is not a database` 整体失败。
- graph digest non-summary 验证前后 `failed` count 不变。
- skill harness check 通过，且新增规则能阻止至少 5 类 false-green 判断。
- state files 让下一位 agent 在 3 分钟内恢复当前状态。

## Open Questions

- 是否允许本次实施自动触发 WeChat key 强制重扫，还是需要用户 same-turn 明确授权？
- 坏 `wcdb_cache` 是否允许程序自动删除单个不可读 cache 文件，还是只允许改名备份？
- `/api/v1/db` 是否保持当前 map 结构并增加旁路 metadata，还是升级为包含 `items/status/error` 的新结构并保留兼容字段？
- 前端 DB 面板是否需要单独的“重新检测 DB 可用性”按钮？
- `all_keys.json` stale 的判定是否只比较 mtime，还是要增加 key-to-current-DB probe 结果作为强信号？
