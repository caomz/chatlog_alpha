# PRD: State-Code Drift Repair and Gated DB Runtime Recovery

## Introduction

当前 `db-runtime-graph-truth-harness-2026-06-12` 出现了明确的 state-code drift：state files 和 `scripts/ralph/prd.json` 声称 US-002/US-003/US-008 已完成，但当前源码里缺少对应实现和测试：

- `internal/wechatdb/wcdbapi/client.go` 没有在最终 `os.Rename(tmpPath, outPath)` 前统一执行 `isReadableSQLite(tmpPath)`。
- `internal/wechatdb/datasource/dbentry/` 不存在。
- `DataSource` interface 没有 `GetDBsWithStatus()`。
- 前端 DB 面板仍会并发 probe `/api/v1/db/tables`。
- `skills/chatlog-http-cli/references/db-truth-chain.md` 和 `graph-truth-chain.md` 不存在。
- `go test ./internal/wechatdb/...` 当前通过但全部是 `[no test files]`，不能证明 US-002/US-003 已实现。

本 PRD 不是重新设计 DB runtime，而是把“假完成态”修回真实可执行链路：先让源码、tests、harness 文档和 state files 对齐，再在用户 same-turn 明确授权后执行 key/cache/service 恢复。后续执行者可能是 MiniMax，模型能力不强，因此 story 必须拆得更细，每个 story 只改一个变量，并且必须写清楚 exact command、可观察结果、失败 notes 规则。

## Audit Findings

- AF-1: 当前 PRD/story 粒度仍偏粗，US-002 同时包含测试、cache 命中、rename 闸门、错误隐私；弱模型容易只做其中一半。
- AF-2: US-003 把 shared type、interface、wcdb classification、HTTP shape、前端依赖放在一起，容易出现编译期循环 import 或后端完成但 UI 未接线。
- AF-3: UI story 依赖 runtime 服务和真实 DB 状态，应拆成静态 parser/compat 验证、不可用状态浏览器验证、恢复后浏览器验证。
- AF-4: Harness 文档恢复不应和 state 写回混在一起；reference 文件存在、SKILL.md precheck 边界、Known False Greens 应分别验收。
- AF-5: US-004 runtime 恢复太大，包含授权、备份、cache、key rescan、restart、tables 验证。必须拆成 blocked runtime stories。
- AF-6: State files 当前已经包含不真实 evidence。必须先有一个 story 专门修正 `scripts/ralph/prd.json` 和 root state，避免 Ralph 继续把 missing code 当 passed。
- AF-7: Graph/digest 验证是只读回归，不应该和 DB runtime recovery 写操作混跑。

## MiniMax Execution Rules

- MER-1: 每次只执行一个 story。不要“顺手”做后续 story。
- MER-2: 一个 story 只能修改该 story 列出的文件范围；遇到缺少前置依赖，写 notes 并停止。
- MER-3: 没有跑完 Acceptance Criteria 中的验证命令，不得把 `passes` 改成 `true`。
- MER-4: 命令失败时，notes 必须包含 exact command、exit status、关键错误行、下一步建议。
- MER-5: Runtime / key / cache / service restart stories 默认 `blocked=true`，除非用户同一轮明确授权。
- MER-6: 不打印聊天正文、表数据、真实 API key、真实 data key。验证只记录 path、mtime、size、count、HTTP status、bucket。
- MER-7: 不批量删除文件或目录。cache 处理只能单个文件；如果需要批量处理，停止并让用户确认。
- MER-8: 不执行 `git commit`、`git push`、`git reset`、`git rebase`、`git merge`。
- MER-9: `health ok`、`all_keys.json exists`、`wcdb_cache exists`、`progress_pct=100`、`last_error=null` 都不能单独作为完成态。
- MER-10: PRD 转 `scripts/ralph/prd.json` 时，runtime stories 必须保留 blocked notes，不能让 batch runner 自动越权执行。

## Goals

- 将 state-code drift 变成可验证事实，并修正不真实的 story completion。
- 用 tests 先锁住 WCDB bad cache 行为，再补后端实现。
- 将 DB listed/queryable 状态从 datasource 透传到 HTTP，再由前端显示。
- 恢复 repo-local harness truth chain，让后续 agent 不再被 false green 误导。
- 在用户授权后，用可回滚步骤恢复 key/cache/service runtime。
- 最终证明 DB tables、前端 DB 面板、graph truth、digest non-summary、harness gates、state files 全部对齐。

## Story Dependency Map

- US-001 -> US-002 -> US-003 -> US-004
- US-005 -> US-006 -> US-007 -> US-008 -> US-009
- US-010 -> US-011 -> US-012
- US-013 depends on US-003..US-012
- US-014..US-018 are runtime stories and default blocked until user authorization
- US-019..US-020 depend on US-018
- US-021 final acceptance depends on all previous stories

## User Stories

### US-001: Drift Baseline Audit
**描述：** 作为维护者，我想用只读命令确认 state files 与源码是否一致，以便后续修复从真实状态开始。

**Acceptance Criteria:**
- [ ] `pwd -P` 输出 `/Volumes/WorkSSD/Dev/chatlog_alpha`。
- [ ] `git status --short` 输出被记录到 notes，只记录路径，不修改、不 revert。
- [ ] `jq '{branchName, stories:[.userStories[] | {id,title,passes,blocked,notes}]}' scripts/ralph/prd.json` 输出被记录。
- [ ] `rg -n "GetDBsWithStatus|DBEntry|core_dbs_unavailable|decrypted database is unreadable|IsReadableSQLite|db-truth-chain|graph-truth-chain" .` 输出被记录。
- [ ] `go test ./internal/wechatdb/...` 输出如果全是 `[no test files]`，notes 必须明确写：当前不能作为 US-002/US-003 完成证据。
- [ ] 不读取、不打印聊天正文、真实 API key、真实 data key。

### US-002: Reset False Passed Stories
**描述：** 作为 Ralph 执行者，我想先修正 `scripts/ralph/prd.json` 中不真实的 passed 状态，以便 batch runner 不会跳过缺失源码。

**Acceptance Criteria:**
- [ ] 如果 US-001 证明 US-002/US-003/US-008 的源码或文档缺失，则对应 story 在 `scripts/ralph/prd.json` 中改为 `passes=false`。
- [ ] 被改回 `passes=false` 的 story 必须写 non-empty `notes`，说明缺少的文件、符号或测试。
- [ ] 不修改 blocked runtime stories 的授权边界；US-004/US-005/US-010 仍不得自动执行。
- [ ] `jq empty scripts/ralph/prd.json` 通过。
- [ ] `jq '.userStories[] | select(.passes==false and .blocked==false) | {id,title,notes}' scripts/ralph/prd.json` 能看到下一条可执行 story 及其 notes。
- [ ] 不修改产品代码。

### US-003: WCDB Readability Tests
**描述：** 作为后端开发者，我想先用 focused tests 锁住 bad cache 和 readable SQLite 的行为，以便实现不会只靠人工判断。

**Acceptance Criteria:**
- [ ] 新增 `internal/wechatdb/wcdbapi/client_test.go` 或等价 focused test 文件。
- [ ] 测试构造一个 header 为 `SQLite format 3` 但 `sqlite_master` 查询失败的文件，断言 `isReadableSQLite(path)` 返回 `false` 且 error 包含 `file is not a database`。
- [ ] 测试构造合法 SQLite 文件，断言 `isReadableSQLite(path)` 返回 `true` 且 error 为 nil。
- [ ] 测试不得使用真实 WeChat DB、真实 `all_keys.json`、真实 data key。
- [ ] `go test -count=1 ./internal/wechatdb/wcdbapi -run 'TestIsReadableSQLite' -v` 通过，并显示新增测试名称。

### US-004: WCDB Final Rename Gate
**描述：** 作为后端开发者，我想在写入 decrypted cache 前加最后一道可读性闸门，以便不可读产物不会进入 `wcdb_cache`。

**Acceptance Criteria:**
- [ ] `ensureDecrypted()` 在任何 `os.Rename(tmpPath, outPath)` 前都会检查 `isReadableSQLite(tmpPath)`。
- [ ] `isReadableSQLite(tmpPath)` 返回 false 或 error 时，`tmpPath` 被删除。
- [ ] `isReadableSQLite(tmpPath)` 返回 false 或 error 时，`outPath` 不被写入或覆盖。
- [ ] `isReadableSQLite(tmpPath)` 返回 false 或 error 时，`c.cache` 不被更新。
- [ ] 返回 error 包含 `all_keys.json` 或 `rescan WeChat keys` 这类可行动提示。
- [ ] 返回 error 不包含真实 key；测试使用 dummy key 并断言 error 不包含该 dummy key。
- [ ] `go test -count=1 ./internal/wechatdb/wcdbapi -run 'TestEnsureDecrypted|TestIsReadableSQLite' -v` 通过。

### US-005: WCDB Cache Hit Invalidation
**描述：** 作为后端开发者，我想让旧 cache 命中时也重新验证可读性，以便坏 cache 不会被继续复用。

**Acceptance Criteria:**
- [ ] 当内存 cache entry 命中且 `entry.outPath` 存在但 `sqlite_master` 不可读时，系统删除该 cache 文件。
- [ ] 删除坏 cache 后，系统重新走 decrypt path。
- [ ] 如果重新 decrypt 的结果仍不可读，系统返回可行动错误。
- [ ] 失败路径中 `c.cache[rel]` 不保留指向坏 cache 的 entry。
- [ ] 新增或更新 focused test 覆盖 cache hit invalidation。
- [ ] `go test -count=1 ./internal/wechatdb/wcdbapi -run 'Test.*Cache' -v` 通过。

### US-006: DB Status Shared Type
**描述：** 作为后端开发者，我想新增独立 shared type，避免 datasource 与 wcdb 包循环 import。

**Acceptance Criteria:**
- [ ] 新增 `internal/wechatdb/datasource/dbentry/dbentry.go`。
- [ ] 定义 `DBEntry`，至少包含 `Path`、`Group`、`Queryable`、`Reason` 字段。
- [ ] `Reason` 字段设计为封闭 token 字符串，不承载 key、wxid、绝对路径。
- [ ] `internal/wechatdb/datasource/datasource.go` 可引用或 re-export 该类型。
- [ ] `go test -count=1 ./internal/wechatdb/datasource/...` 通过。
- [ ] `go test -count=1 ./internal/wechatdb/...` 编译通过，无 import cycle。

### US-007: DataSource Listed/Queryable Contract
**描述：** 作为后端开发者，我想让 datasource 层同时返回 listed DB 和 queryable 状态，以便 HTTP 层不再自己猜测。

**Acceptance Criteria:**
- [ ] `DataSource` interface 新增 `GetDBsWithStatus() (map[string][]DBEntry, error)` 或等价方法。
- [ ] 旧 `GetDBs()` 保留，并继续返回 `map[string][]string`。
- [ ] `GetDBs()` 可由 `GetDBsWithStatus()` 派生，旧调用方不需要理解 status。
- [ ] `go test -count=1 ./internal/wechatdb/datasource/...` 通过。
- [ ] `go build ./...` 通过。

### US-008: WCDB DB Classification
**描述：** 作为后端开发者，我想让 wcdb datasource 按固定规则判断 DB 可查询性，以便 UI 和 API 显示同一个真值。

**Acceptance Criteria:**
- [ ] wcdb datasource 对每个 listed DB 返回 `Queryable=true/false`。
- [ ] 缺失文件返回 reason `file_not_found`。
- [ ] `os.Stat` 非 not-exist 错误返回 reason `stat_error`。
- [ ] SQLite 不可读或 `CanQueryDB` 失败返回 reason `core_db_unreadable` 或等价封闭 token。
- [ ] reason 不包含 `wxid_`、真实 key、绝对路径。
- [ ] focused test 覆盖 missing file、stat error、unreadable SQLite 至少两个场景。
- [ ] `go test -count=1 ./internal/wechatdb/datasource/wcdb -v` 通过。

### US-009: HTTP DB Status Response
**描述：** 作为 API 调用方，我想 `/api/v1/db` 返回兼容路径列表和不可用 metadata，以便旧客户端不破、新客户端能读到真值。

**Acceptance Criteria:**
- [ ] `/api/v1/db` 响应包含旧客户端可解析的 DB 路径列表信息。
- [ ] `/api/v1/db` 响应包含 `unavailable` 数组或等价字段，元素至少有 `group`、`file`、`reason`。
- [ ] 核心库 `session`、`contact`、`message` 任一不可查询时，响应包含 `core_dbs_unavailable=true` 或等价字段。
- [ ] 不可用 metadata 不包含真实 key、wxid、聊天内容。
- [ ] HTTP handler 出错时继续使用项目现有 `errors.Err` 风格。
- [ ] `go test -count=1 ./internal/chatlog/...` 中相关 package 通过，或记录无 HTTP unit test 时用 `go build ./...` + runtime curl 作为替代验证。
- [ ] `go build ./...` 通过。

### US-010: Frontend DB List Parser
**描述：** 作为前端维护者，我想让 DB 面板同时支持新旧 `/api/v1/db` 响应，以便服务未重启或旧 binary 时页面不空白。

**Acceptance Criteria:**
- [ ] `loadDBList()` 优先解析新 shape。
- [ ] `loadDBList()` 在旧 `map[group][]file` shape 下仍能展示 DB 分组和文件。
- [ ] 前端不再批量并发 probe 所有 `/api/v1/db/tables` 判断可查询性。
- [ ] 使用静态 JS 解析检查，例如提取 `loadDBList` 后执行 `new Function(loadDBListString)`，无语法错误。
- [ ] 若新增 helper function，helper function 也通过同等静态解析检查。
- [ ] 不启动浏览器、不触碰 runtime。

### US-011: Frontend Unavailable State UI
**描述：** 作为前端用户，我想在核心 DB 不可用时看到明确错误，而不是泛化 `加载失败`。

**Acceptance Criteria:**
- [ ] 当前端收到 `core_dbs_unavailable=true` 时，页面展示 `数据库当前不可用`。
- [ ] 同一错误区域展示 `请重扫 key 或检查 wcdb cache` 或等价可操作提示。
- [ ] 非核心 DB 不可用时，只在对应 DB item 显示不可查询状态。
- [ ] 不可查询 badge 或 title 只显示封闭 reason token，不显示真实路径/key。
- [ ] 使用 agent-browser 打开 `http://127.0.0.1:5030/`，进入数据库调试 / DB 面板；在后端返回不可用状态时能看到上述文案。
- [ ] 浏览器控制台无未捕获异常。
- [ ] 验证过程中不打开表数据、不打印聊天内容。

### US-012: Harness Precheck Boundary
**描述：** 作为后续 agent，我想让 `skills/chatlog-http-cli/SKILL.md` 明确区分 availability precheck 与完成态，以便不再把 `/health` 当 DB 修复成功。

**Acceptance Criteria:**
- [ ] `skills/chatlog-http-cli/SKILL.md` 明确写出 `/health` 和 `/api/v1/ping` 只是 availability precheck。
- [ ] `SKILL.md` 明确写出 DB 完成态需要 `/api/v1/db/tables` 或等价 DB 可查询证据。
- [ ] `SKILL.md` 明确写出 graph 完成态不能只看 `progress_pct=100`。
- [ ] `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs` 通过。
- [ ] 文档不包含真实聊天正文、真实 API key、真实 data key。

### US-013: DB Truth Chain Reference
**描述：** 作为后续 agent，我想有一份 DB/key/cache truth chain reference，以便遇到 `file is not a database` 时按固定步骤排障。

**Acceptance Criteria:**
- [ ] 新增 `skills/chatlog-http-cli/references/db-truth-chain.md`。
- [ ] 文档包含 AppleDouble、stale key、bad cache 三类解释。
- [ ] 文档包含 `isReadableSQLite` / `sqlite_master` 是 DB 可查询真值。
- [ ] 文档包含只读诊断命令模板，且命令只输出 path、mtime、size、count、HTTP status、error bucket。
- [ ] 文档明确 key rescan、cache 清理、service restart 属于 destructive runtime recovery，需要 same-turn 授权。
- [ ] `grep -rn -E 'wxid_[a-z0-9]{8,}|sk-[a-zA-Z0-9]{16,}|[a-f0-9]{32,}' skills/chatlog-http-cli/references/db-truth-chain.md` 无命中。

### US-014: Graph Truth Chain Reference
**描述：** 作为后续 agent，我想有一份 temporal graph truth chain reference，以便不再把 progress 或 health 当完成态。

**Acceptance Criteria:**
- [ ] 新增 `skills/chatlog-http-cli/references/graph-truth-chain.md`。
- [ ] 文档包含 HTTP `/api/v1/graph/status?format=json` 字段解释。
- [ ] 文档包含 readonly SQLite failed bucket 验证方法。
- [ ] 文档包含 digest non-summary path 不扰队列的验证方法。
- [ ] 文档明确 `summary=true` 是 quota-sensitive，不进入默认验收。
- [ ] 文档明确 `progress_pct=100` 只表示 `pending=0`，不表示 `failed=0`。
- [ ] 敏感内容 grep 检查无真实 ID/key 泄漏。

### US-015: Feedback Audit False Greens
**描述：** 作为后续 agent，我想把常见 false green 写进 feedback audit，以便 review 时能直接拦截假完成。

**Acceptance Criteria:**
- [ ] `skills/chatlog-http-cli/references/feedback-audit.md` 新增 `Known False Greens` 段。
- [ ] 至少列出 `health ok`、`all_keys.json exists`、`wcdb_cache exists`、`progress_pct=100`、`last_error=null`、`processing=N`、`/api/v1/db lists paths`、`report graph exit 0`。
- [ ] 每个 false green 都给出对应 truth command 或 truth condition。
- [ ] `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs` 通过。
- [ ] 不修改产品代码。

### US-016: State Sync After Code And Harness
**描述：** 作为维护者，我想在代码和文档都验证后同步 root state，以便下一轮 session 不再读到旧的假 evidence。

**Acceptance Criteria:**
- [ ] `scripts/ralph/prd.json` 中已完成 story 的 `passes=true` 都有对应 command evidence。
- [ ] 未完成 story 保持 `passes=false`，notes 非空。
- [ ] Runtime destructive stories 保持 `blocked=true`，直到用户授权。
- [ ] `feature_list.json` evidence 与当前源码、tests、harness 文档一致。
- [ ] `progress.md` 记录 drift 修复事实、改动文件、验证命令、未验证项。
- [ ] `session-handoff.md` 记录下一会话可直接执行的 read-only commands 和 blocked runtime boundary。
- [ ] `jq empty feature_list.json && jq empty scripts/ralph/prd.json` 通过。
- [ ] `node scripts/check-root-harness.mjs` 通过。

### US-017: Runtime Recovery Authorization Preflight
**描述：** 作为维护者，我想在执行 key/cache/service 操作前确认用户授权和当前 runtime 状态，以便不会越权破坏本地数据链路。

**Acceptance Criteria:**
- [ ] 用户在同一轮对话中明确授权执行 key rescan、cache 单步处理、service restart。
- [ ] 若没有授权，本 story 保持 `blocked=true`，notes 写明等待授权。
- [ ] 授权后先运行 `curl -sS --max-time 8 http://127.0.0.1:5030/health`，记录 status。
- [ ] 授权后运行 `/api/v1/db` 和核心三库 `/api/v1/db/tables` preflight，只记录 HTTP status、count、错误摘要。
- [ ] 授权后记录 `all_keys.json` path、size、mtime，不打印内容。
- [ ] 授权后记录 `~/.chatlog/wcdb_cache` count、mtime range、坏 cache bucket，不打印 DB 内容。
- [ ] 不删除、不重启、不重扫；本 story 只做授权和 preflight。

### US-018: Backup Keys And Quarantine One Cache File
**描述：** 作为维护者，我想先备份 key 并单步处理一个坏 cache 文件，以便恢复过程可回滚。

**Acceptance Criteria:**
- [ ] 当前 `all_keys.json` 被复制到带时间戳的备份路径。
- [ ] 备份后记录 backup path、size、mtime，不打印 key 内容。
- [ ] 选择一个坏 cache 文件作为 canary，只记录 basename、size、mtime。
- [ ] 处理 canary cache 时只允许单文件删除或单文件改名备份；不得批量删除。
- [ ] 处理后再次验证该 cache path 的存在状态或 quarantine path 状态。
- [ ] 如果需要处理多个 cache 文件，停止并让用户确认，不在本 story 批量处理。

### US-019: Force WeChat Key Rescan
**描述：** 作为维护者，我想在已有备份后强制重扫 WeChat key，以便 stale key 能被替换。

**Acceptance Criteria:**
- [ ] 本 story 只在 US-017 授权和 US-018 备份完成后执行。
- [ ] 执行项目已有 key scan / helper 命令，不新增全局安装或系统级改动。
- [ ] 命令输出不得打印真实 key；如工具默认打印 key，必须改用安全模式或停止。
- [ ] 新 `all_keys.json` 存在。
- [ ] 新 `all_keys.json` 的 mtime 晚于备份文件 mtime。
- [ ] 只记录新文件 path、size、mtime。
- [ ] 如果 key rescan 失败，notes 记录 exact command、exit status、关键错误行、下一步。

### US-020: Restart Service With New Binary
**描述：** 作为维护者，我想让 5030 服务加载当前新 binary，以便 HTTP route、DB status response 和 frontend static assets 生效。

**Acceptance Criteria:**
- [ ] `go build -o bin/chatlog ./cmd/chatlog` 或项目现有 build gate 成功。
- [ ] 记录当前 `chatlog-alpha` tmux/session/listener 状态，只记录 pid/count，不打印 env secret。
- [ ] 使用项目约定方式重启服务，例如 `./bin/chatlog serve --config-dir .cache/daily-report-config`。
- [ ] 重启后 `curl -sS --max-time 8 http://127.0.0.1:5030/health` 返回 `{"status":"ok"}`。
- [ ] 重启后 `/api/v1/db` 返回新 shape 或明确兼容 shape。
- [ ] 如果重启失败，notes 记录 exact command、exit status、关键错误行、回滚方式。

### US-021: Core DB Tables Runtime Verification
**描述：** 作为维护者，我想用 HTTP 验证核心 DB 真实可查询，以便确认 key/cache/service 恢复成功。

**Acceptance Criteria:**
- [ ] `/api/v1/db/tables?group=session&file=session.db&format=json` 返回 HTTP 200。
- [ ] session tables 数量大于 0。
- [ ] `/api/v1/db/tables?group=contact&file=contact.db&format=json` 返回 HTTP 200。
- [ ] contact tables 数量大于 0。
- [ ] `/api/v1/db/tables?group=message&file=message_0.db&format=json` 返回 HTTP 200。
- [ ] message tables 数量大于 0。
- [ ] 验证只记录 status 和 table count，不打印表数据。
- [ ] 如果任一核心 DB 失败，notes 记录 exact command、HTTP status、关键错误行、下一步。

### US-022: Frontend Recovery Browser Verification
**描述：** 作为用户，我想在浏览器里确认 DB 面板恢复，以便证明前后端闭环可用。

**Acceptance Criteria:**
- [ ] 使用 agent-browser 打开 `http://127.0.0.1:5030/`。
- [ ] 进入数据库调试 / DB 面板后，页面列出 DB 分组。
- [ ] 选择或加载 `session/session.db`，页面显示 tables 列表，不出现 `file is not a database`。
- [ ] 选择或加载 `contact/contact.db`，页面显示 tables 列表，不出现 `file is not a database`。
- [ ] 选择或加载 `message/message_0.db`，页面显示 tables 列表，不出现 `file is not a database`。
- [ ] 页面无未捕获控制台错误。
- [ ] 验证不点击表数据、不打印聊天内容。

### US-023: Graph Status Readonly Regression
**描述：** 作为维护者，我想确认 DB runtime 恢复没有让 temporal graph 出现 DB 相关失败，以便 graph health 不被误判。

**Acceptance Criteria:**
- [ ] `curl -sS --max-time 12 'http://127.0.0.1:5030/api/v1/graph/status?format=json'` 返回可解析 JSON。
- [ ] status 中 `last_error` 为 `null` 或空字符串。
- [ ] 记录 `failed`、`pending`、`processing`、`progress_pct`、`failed_buckets`。
- [ ] readonly SQLite 查询 `graph_source_records` failed bucket。
- [ ] `file is not a database` / `db_not_database` bucket 为 0。
- [ ] 不执行 `graph/resume`、`graph/rebuild`、`graph/pause`、`graph/config`。
- [ ] 不打印 graph source content、prompt、聊天正文。

### US-024: Digest Non-Summary Queue Invariance
**描述：** 作为维护者，我想确认 `chatlog report graph --days 7` 仍是 zero-model-call 验证路径，并且不扰动 graph failed count。

**Acceptance Criteria:**
- [ ] 运行前记录 HTTP graph failed count 和 readonly SQLite failed count。
- [ ] `go run . report graph --days 7` 成功。
- [ ] 输出 metadata 或 HTTP response 中 `summary_used=false`。
- [ ] 只验证 digest 文件 path、mtime、size、H2 section count。
- [ ] digest 文件包含 7 个 `## ` H2 section。
- [ ] 运行后 HTTP failed count 与运行前一致。
- [ ] 运行后 SQLite failed count 与运行前一致。
- [ ] 不打印 digest 正文，不执行 `--summary`。

### US-025: Final End-To-End Acceptance
**描述：** 作为维护者，我想用最终闭环证明源码、runtime、UI、graph、harness 和 state files 全部对齐，以便可以安全结束本 feature。

**Acceptance Criteria:**
- [ ] `go test -count=1 ./internal/wechatdb/... ./internal/chatlog/temporalgraph` 通过。
- [ ] `go build ./...` 通过。
- [ ] `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs` 通过。
- [ ] `node scripts/check-root-harness.mjs` 通过。
- [ ] `./init.sh` 通过。
- [ ] 核心三库 `/api/v1/db/tables` 均 HTTP 200 且 tables 数量大于 0。
- [ ] 前端 DB 面板可加载核心库 tables，无 `file is not a database` 和未捕获控制台错误。
- [ ] graph status 可读，`last_error=null`，DB 相关 failed bucket 为 0。
- [ ] digest non-summary path 运行前后 graph failed count 不增加。
- [ ] `feature_list.json`、`progress.md`、`session-handoff.md` 记录最终状态、验证证据、未验证项、风险和回滚方式。
- [ ] 未执行 `git commit`、`git push`、`git reset`、`git rebase`、`git merge`，除非用户另行明确授权。

## Functional Requirements

- FR-1: 系统必须在 WCDB 解密 cache 写入前验证 `sqlite_master` 可读性。
- FR-2: 系统必须在 cache 命中时继续验证 cache 可读性，不得信任旧 cache。
- FR-3: 系统必须在解密产物不可读时删除临时产物，不写入目标 cache，不更新内存 cache entry。
- FR-4: 系统必须返回可行动错误，提示 stale `all_keys.json`、重扫 key、备份后清理 wcdb cache，不得包含真实 key。
- FR-5: `/api/v1/db` 必须表达 listed 与 queryable 的区别，并保留旧客户端兼容路径。
- FR-6: `/api/v1/db` 的不可用 reason 必须是封闭 token，不得包含 `wxid_`、真实 key、绝对路径或聊天内容。
- FR-7: 前端 DB 面板必须支持新 response shape，也必须兼容旧 `map[group][]file` shape。
- FR-8: 前端 DB 面板不得批量并发 probe 所有 `/api/v1/db/tables` 来判断可查询性。
- FR-9: repo-local harness 文档必须记录 DB/key/cache truth chain 与 graph truth chain。
- FR-10: `scripts/ralph/prd.json` 的 `passes=true` 必须来自当前源码和命令验证，不得只来自历史叙述。
- FR-11: Runtime key/cache/service 操作必须 same-turn 授权，且执行前必须先备份。
- FR-12: Runtime 恢复不得批量删除文件；如必须批量处理，必须停止并让用户手动确认。
- FR-13: Graph validation 必须保持只读，除非另一个 story 明确授权 graph write endpoint。
- FR-14: Digest 默认 path 必须保持 `summary_used=false`，不得消耗模型 quota。

## Non-Goals

- 不重新设计 WeChat key 扫描算法。
- 不清空或重建 temporal graph 数据库。
- 不盲目 requeue graph failed rows。
- 不调用 `chatlog report daily --vision` 或 `chatlog report daily --summary`。
- 不执行 graph digest `--summary` 模型摘要路径。
- 不打印聊天正文、表数据、真实 API key、真实 data key。
- 不批量删除 cache 文件或目录。
- 不自动 commit、push、reset、rebase、merge。
- 不把 graph failed count 清零作为本 PRD 的成功标准；只要求 DB 问题不进入 graph failed bucket，并且完成态判断不误报。

## Design Considerations

- DB 面板文案要短、明确、可操作：
  - `数据库当前不可用`
  - `请重扫 key 或检查 wcdb cache`
- 核心库失败要作为全局 banner 展示；非核心库失败只标记单个 DB item。
- 不在 UI 里展示真实 user id、真实 key、绝对隐私路径；必要时只显示 basename 或固定 reason token。
- 若旧服务仍运行旧 binary，前端必须能 fallback 读取旧 `/api/v1/db` shape，避免空白面板。

## Technical Considerations

- 关键路径：
  - `internal/wechatdb/wcdbapi/client.go`
  - `internal/wechatdb/wcdbapi/client_test.go`
  - `internal/wechatdb/datasource/datasource.go`
  - `internal/wechatdb/datasource/dbentry/dbentry.go`
  - `internal/wechatdb/datasource/wcdb/datasource.go`
  - `internal/wechatdb/wechatdb.go`
  - `internal/chatlog/database/service.go`
  - `internal/chatlog/http/route.go`
  - `internal/chatlog/http/static/index.htm`
  - `skills/chatlog-http-cli/SKILL.md`
  - `skills/chatlog-http-cli/references/*.md`
  - `scripts/ralph/prd.json`
  - `feature_list.json`
  - `progress.md`
  - `session-handoff.md`
- `isReadableSQLite(path)` 已存在，应作为 DB 可查询 truth check。
- 共享 DB status 类型建议放到独立小包，避免 `datasource` 和 `wcdb` 循环 import。
- 新 HTTP response 应采用 additive 设计，保留旧字段或旧 shape fallback。
- `chatlog-alpha` 服务重启建议使用已有 `tmux` session 和 `./bin/chatlog serve --config-dir .cache/daily-report-config` 路径，但必须在用户授权后执行。
- 如果 validation 发现 `go test ./internal/wechatdb/...` 仍是 `[no test files]`，不能把 US-002/US-003 记为完成。

## Success Metrics

- Story 粒度从 10 条粗 story 拆成 25 条单变量 story，适合 MiniMax 批量逐条执行。
- `go test -count=1 ./internal/wechatdb/...` 至少运行到新增 focused tests，并全部通过。
- 核心 DB `/api/v1/db/tables` 成功率从 0/3 提升到 3/3。
- 前端 DB 面板在核心库不可用时显示明确 banner，在恢复后显示 tables 列表。
- `node scripts/check-root-harness.mjs` 与 `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs` 均通过。
- `feature_list.json`、`progress.md`、`session-handoff.md` 能让下一位 agent 在 3 分钟内恢复真实状态。

## Open Questions

- US-017 之后的 runtime stories 是否由 agent 在 same-turn 授权后执行，还是由用户手动执行后让 agent 只做验证？
- 坏 `wcdb_cache` 文件在恢复阶段应优先单个删除，还是单个改名备份以便回滚？
- `/api/v1/db` 新响应是否采用 `{dbs, unavailable, core_dbs_unavailable}`，还是另起 `/api/v1/db/status` 避免影响旧客户端？
- 前端 DB 面板是否需要增加“重新检测 DB 可用性”按钮，还是依赖刷新页面重新拉取 `/api/v1/db`？
- 如果 key 重扫后核心 DB 仍不可读，下一步是回滚 key/cache，还是进入更深的 WeChat DB 解密链路诊断？
