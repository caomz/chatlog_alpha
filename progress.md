# Progress

Last Updated: 2026-06-20

## Claude Code guard-hooks / claude-code-hooks (US-013, 2026-06-20)

- **Current State**: active feature is now `claude-code-guard-hooks-2026-06-19` with `status=done`. PRD `scripts/ralph/prd.json` (branch `ralph/claude-code-guard-hooks`, 15 stories): US-001..US-012 `passes=true`; US-013 (this state record) now `passes=true`; US-014 (zero product-code regression gate) still `passes=false blocked=false`; US-015 `blocked=true` (runtime/manual — needs a real interactive Claude Code session to trigger each interception and observe hot-reload + stderr feedback; `claude --print` subprocesses cannot reliably self-verify hot-reload). 8 Claude Code agent hooks live under `.claude/hooks/` and are wired into `.claude/settings.json`.
- **What changed**:
  - `feature_list.json`: `active_feature_id` → `claude-code-guard-hooks-2026-06-19`; new feature entry added (name/title/description/scope/done_criteria/evidence/status=done, string-array shape matching `graph-knowledge-digest-2026-06-10`).
  - `progress.md`: this section.
  - `session-handoff.md`: Next Session path for the guard-hooks feature (hook inventory, test script path, key risks).
  - No product code (`internal/`, `cmd/`) touched in this story; only state files.
- **Verification Evidence**:
  - `bash .claude/hooks/test_hooks.sh` → `assertions run: 76`, final line `ALL HOOK TESTS PASSED`, exit 0 (covers US-001..US-009 positive/negative cases offline; the two `Blocked: ... semantic/test` lines are the guard-quota hook's own stderr captured by US-007 assertions, expected).
  - `python3 -c "import json; json.load(open('.claude/settings.json'))"` → `settings.json json.load OK` (exit 0).
  - `git check-ignore .claude/settings.json` → rc=1 (not gitignored, commit-able).
  - `python3 -c "import json; json.load(open('feature_list.json'))"` → exit 0 (feature_list.json valid after the new entry).
- **Not Verified**:
  - US-015 end-to-end interception in a real interactive Claude Code session (hot-reload即生效 + 模型下一轮可见 stderr) — `blocked=true`, not run by the Ralph `claude --print` subprocess. Must be verified manually by the user.
  - US-014 regression gate (zero product-code change + `node scripts/check-root-harness.mjs` + `./init.sh`) is the next executable story; not part of US-013.
- **Blockers/Risks**:
  - Hot-reload has no safety gate; a malformed hook self-blocking is mitigated only by the narrow `.claude/hooks/*` + `.claude/settings.json` allowlist in block-private-writes. See docs/claude-code-hooks.md risk section.
  - Ralph calls git via subprocess in `scripts/ralph/ralph.py`, which bypasses these PreToolUse hooks (hooks only intercept the agent's own tool calls, not child-process git). Documented intentionally.
- **Recommended Next Step**: implement US-014 (regression verification), then hand US-015 to the user for a real interactive Claude Code session to trigger each interception path.

## DB runtime core query recovery (2026-06-17)

- **Current State**: active feature is now `db-runtime-core-query-recovery-2026-06-17` with `status=done`. User gave same-turn authorization for the previously blocked runtime operation. The live `127.0.0.1:5030` service is restarted from the rebuilt `bin/chatlog` in tmux session `chatlog-alpha`; core DB table-list queries for `session`, `contact`, and `message_0` are now HTTP 200. No chat rows, report bodies, model prompts, key contents, or private media were printed.
- **What changed**:
  - `internal/wechatdb/wcdbapi/client.go`: `resolveDBPath()` now maps bare relative filenames such as `session.db` through the requested group, so `group=session&file=session.db` resolves to `db_storage/session/session.db` instead of `db_storage/session.db`.
  - `internal/wechatdb/wcdbapi/client.go`: `ensureDecrypted()` now validates the decrypted temp file with `isReadableSQLite()` before `os.Rename(tmpPath, outPath)`. Unreadable decrypt output is removed and returned as an actionable stale-key/cache error instead of poisoning `wcdb_cache`.
  - `internal/wechatdb/wcdbapi/client_test.go`: added focused coverage for bare-file group resolution, explicit relative subdirs, and default-kind resolution.
  - `.gitignore`: added `.gstack/` so local tool metadata does not dirty the repo.
  - `feature_list.json`, `progress.md`, and `session-handoff.md`: recorded the authorized recovery, verification evidence, privacy boundary, and next restart path.
- **Verification Evidence**:
  - Pre-fix live truth: `/api/v1/db/tables?group=session&file=session.db`, `contact/contact.db`, and `message/message_0.db` returned HTTP 500 `stat .../db_storage/<file>: no such file or directory`; the source files existed under `db_storage/session/session.db`, `db_storage/contact/contact.db`, and `db_storage/message/message_0.db`.
  - `go test -count=1 ./internal/wechatdb/wcdbapi` -> PASS.
  - `./init.sh --full` -> PASS after code fix: root harness **80/80**, repo-local skill harness **29/29**, `go test ./...` PASS, `make build` PASS.
  - Runtime restart: old listener PID `23936` stopped; new tmux listener PID `1464` started with `./bin/chatlog serve --config-dir .cache/daily-report-config`; `lsof -nP -iTCP:5030 -sTCP:LISTEN` shows `127.0.0.1:5030`; `/health` returns `{"status":"ok"}`.
  - Core DB table-list recovery on the new binary:
    - `session/session.db` -> HTTP 200, `table_count=7`.
    - `contact/contact.db` -> HTTP 200, `table_count=16`.
    - `message/message_0.db` -> HTTP 200, `table_count=406`.
- **Not Verified (privacy/quota/runtime boundary)**:
  - No WeChat key rescan was executed because the core query chain recovered without it. `all_keys.json` contents were never printed.
  - No `wcdb_cache` file was deleted or moved. Existing bad cache artifacts for non-core/old attempts may remain, but the new guard prevents newly generated unreadable decrypt output from replacing cache entries.
  - No `/api/v1/db/query`, `/api/v1/db/data`, `/api/v1/history`, `/api/v1/search`, `/api/v1/sessions`, or `/api/v1/contacts` row/body output was read.
  - No graph write endpoint (`resume`, `rebuild`, `pause`, `config`), model call, Hermes push, or daily report generation was run.
- **Blockers/Risks**:
  - None for the scoped core DB recovery.
  - If a future DB table query returns `decrypted database is unreadable; all_keys.json may be stale or invalid`, then key rescan/cache cleanup becomes the next justified step. Until that explicit error appears, do not batch-delete cache or re-scan keys by default.
- **Recommended Next Step**: monitor future DB failures by first checking `/api/v1/db/tables` status and the explicit `decrypted database is unreadable` error. Do not rescan keys or clean cache unless that error appears or the user explicitly requests deeper recovery.

## Workspace tidy state repair (2026-06-17)

- **Current State**: active feature is now `workspace-tidy-2026-06-17` with `status=done`. The previous active feature `db-runtime-graph-truth-harness-2026-06-12` is marked `discarded` because its recovery implementation was not preserved as the current main-line task; its diagnostic evidence and PRD history remain available for future reference. Current `main` was already clean and synchronized with `origin/main` at commit `4e101144` before this state repair.
- **What changed**:
  - `feature_list.json`: `active_feature_id` now points to `workspace-tidy-2026-06-17`; the new workspace tidy feature records the commit/push/harness evidence; `db-runtime-graph-truth-harness-2026-06-12` now has `status=discarded` and a historical next step.
  - `progress.md`: this section records the state repair so future agents do not resume the discarded DB runtime PRD as the active feature.
  - `session-handoff.md`: current objective refreshed to the clean-worktree truth; DB runtime recovery is explicitly a future authorized task, not current active work.
- **Verification Evidence**:
  - `git status -sb` before repair: `## main...origin/main`; `git status --short` empty.
  - `git rev-list --left-right --count @{u}...HEAD` before repair: `0 0`.
  - `jq '.active_feature_id as $id | {active_feature_id:$id, active:(.features[] | select(.id==$id) | {id,status,next_step,evidence_count:(.evidence|length)}), feature_count:(.features|length)}' feature_list.json` before repair confirmed stale active `db-runtime-graph-truth-harness-2026-06-12 status=in_progress`.
  - `git check-ignore -v reports reports.backup-20260606_124656 openai_prmpt.md` confirms private/local artifacts remain ignored by `.gitignore`.
- **Not Verified**:
  - No DB runtime recovery was attempted. No key rescan, bad cache cleanup, service restart, graph resume/rebuild, model call, or private report inspection was performed.
  - This repair does not prove `session/contact/message_0` DB queryability. It only fixes repo-local state truth after the worktree cleanup.
- **Blockers/Risks**:
  - DB recovery remains a separate, potentially destructive runtime task requiring same-turn user authorization before touching keys, cache, or service processes.
  - The discarded DB runtime PRD still exists as historical evidence in `tasks/`, `scripts/ralph/prd.json`, `progress.md`, and `archive/`; future agents must treat it as history unless the user explicitly reopens it.
- **Recommended Next Step**: commit and push this state repair, then keep `main` clean. If DB runtime recovery is needed later, start a fresh scoped task from the archived evidence and current runtime truth.

## US-009 Root state 与完成态记录 (2026-06-16 18:55)

- **Current State**: active feature `db-runtime-graph-truth-harness-2026-06-12` 已 7/10 stories 通过(US-001/002/003/006/007/008/009),US-004/005/010 仍 `blocked=true`(待用户 same-turn 授权强制重扫 WeChat key + 清理坏 wcdb cache + 重启服务等破坏性运行态操作)。`scripts/ralph/prd.json` 中 10 个 story 状态:7 个 `passes=true notes="" retryCount=0`,3 个 `passes=false blocked=true`(US-004/005/010)。`feature_list.json` 中 `db-runtime-graph-truth-harness-2026-06-12` `status: planned → in_progress`,evidence 数组填入 7 个 passed story 的命令级证据。
- **What changed**:
  - `feature_list.json` `db-runtime-graph-truth-harness-2026-06-12` 段:
    - `evidence` 从 `[]` → 7 个 story 的命令级 evidence 数组(US-001 只读诊断 / US-002 isReadableSQLite 闸门 / US-003 DB 状态区分 / US-006 graph 真值链 / US-007 digest 不扰队列 / US-008 skill 写回 / US-009 本次 state 记录),每条含 `command` + `result` 字段,evidence 文本明确不含 wxid/真实 API key/真实 data key。
    - `next_step` 替换为精确描述(7/10 通过 + 3 blocked 待用户授权)。
    - `status` 从 `planned` → `in_progress`(因为 3 个 blocked story 还要等用户授权;`done` 在 US-004 解 blocked 且 US-005/US-010 全部 passes=true 后才能标)。
  - `progress.md` 本段新增 US-009 收口段(本节)。
  - `session-handoff.md` "Current Objective" 段下文追加 US-009 段 + "Ralpha Automation Handoff" 段补 US-009 收口信息。
- **Verification Evidence**:
  - `jq empty feature_list.json` → PASS, JSON 合法。
  - `node scripts/check-root-harness.mjs` → **80/80 PASS** (与 US-008 提交时 baseline 一致,US-009 未引入新 harness 项)。
  - `jq '.userStories | length' scripts/ralph/prd.json` → `10` (US-001..US-010)。
  - `jq '.userStories[] | select(.id=="US-009") | {id, passes, notes, blocked, retryCount}' scripts/ralph/prd.json` → `{passes: true, notes: "", blocked: false, retryCount: 0}` 三步收口满足 Codebase Pattern 15/27 "修复类 story 必须 `passes=true` + `notes=""` + `retryCount=0`"。
  - `git status --porcelain | grep -E '^(\?\?|.*) (reports|reports\.backup|.cache|logs|outputs|\.env)'` → 全部为 `?? reports`(symlink,预存在,不入库) + `?? reports.backup-20260606_124656/`(预存在 dirty,与本 story 无关),无新增。
  - `grep -rE 'wxid_[a-z0-9]{8,}|sk-[a-zA-Z0-9]{16,}|[a-f0-9]{32,}' feature_list.json` → 0 命中,evidence 不含真实聊天 ID/真实 API key/真实 data key。
- **Not Verified**:
  - **未跑 git commit**: 本 story 严格不动 git(per AC#6 "本 story 不执行 git commit/push/reset/rebase/merge")。Ralph 自动 commit 留给 `scripts/ralph/ralph.py` 在 Validator 成功后执行;若要在此会话完成 feature 收口并入仓,需用户明确授权或由 ralph.py 自动接管。
  - **未跑 `./init.sh --full` / `./init.sh --runtime`**: US-009 是 state files 收口,无 Go 代码改动,quick gate + root harness 已足够覆盖。
  - **US-004/005/010 验证未跑**: 仍 blocked 待用户 same-turn 授权;progress.md / session-handoff.md / prd.json 中均明确标 `blocked=true` 并写明原因(破坏性运行态操作 + 依赖 US-004)。
  - **未把 `?? reports` symlink 加入 `.gitignore`**: 仓库惯例保留 symlink 行,US-007 已记录"`?? reports` 是 symlink 行不是产物污染";US-009 不改变此边界。
- **Blockers-Risks**:
  - **Risk**: 启动前已有 dirty 文件(`M AGENTS.md / M internal/chatlog/manager.go / M scripts/check-root-harness.mjs` 等)属于前序 story 与运行时残留,US-009 不动它们,留给后续合并时统一处理。
  - **Risk**: feature_list.json 中 `status` 是 `in_progress` 而非 `done`,因为 US-004/005/010 仍 blocked。任何"完成"判定都要等 US-010 端到端验收通过 + 用户授权后才成立。
  - **Blocker**: US-004 解除 blocked 的前提(per PRD + 当前 PRD notes): 用户 same-turn 明确授权 + 仅人工或本交互会话执行 + 须先备份 all_keys.json + 单步删 cache(一次一个) + 每步复验。Ralph 自主循环不得自动执行。
- **Next**:
  1. 下一个 Ralph 可自主执行项:**无**(7/10 done,3/10 blocked)。Ralph 循环应输出 `<promise>COMPLETE</promise>` 或等待用户授权 US-004。
  2. 用户手动接管:US-004 解除 blocked → 备份 all_keys.json → 单步删坏 cache → 重启服务 → US-005 前端面板闭环 → US-010 端到端验收。完整诊断命令见 session-handoff.md "Next-session diagnostic commands" 段。
  3. 兜底验证命令(下次会话启动即跑):
     - `jq empty feature_list.json && node scripts/check-root-harness.mjs` → 期望 80/80。
     - `curl -sS --max-time 8 http://127.0.0.1:5030/health` → 期望 `{"status":"ok"}`(只读 precheck,不是 DB 可查询真值)。
     - `curl -sS 'http://127.0.0.1:5030/api/v1/graph/status?format=json' | jq '{failed, failed_buckets, last_error, progress_pct}'` → 期望 failed=531 / progress_pct=100 不代表 done。

## US-006 Temporal graph 健康状态真值链 (2026-06-16 18:55)

- **HTTP 真值**: `curl -sS --max-time 12 'http://127.0.0.1:5030/api/v1/graph/status?format=json'` 返回 `enabled=true paused=false running=false history_queued=true enqueue_running=false workers=3 enqueue_workers=1 effective_workers=1 adaptive_level=stable`,`source_count=22118 entity_count=26895 relation_count=28141 event_count=18247 fact_count=27154 pending=0 processing=0 processed=21587 failed=531 progress_pct=100 last_updated_at=2026-06-14T09:01:35+08:00`,**`last_error=null`**,`failed_buckets={auth_error=141, before_request_timeout=0, config_error=249, empty_graph=0, json_decode_error=10, network_timeout=97, non_retryable_request=0, rate_limited=0, sensitive_input_1026=0, sensitive_output_1027=27, unclassified=7}`。
- **SQLite 真值(等价证据)**:`sqlite3 -readonly` 直接 query `graph_source_records WHERE status='failed'` + Go-side mirror `ClassifyFailedError` 闭包重新分类(同 token 列表 + 同 orderedBuckets 顺序),结果与 HTTP `failed_buckets` **完全一致**: `total=531 config_error=249 network_timeout=97 before_request_timeout=0 rate_limited=0 auth_error=141 json_decode_error=10 empty_graph=0 sensitive_input_1026=0 sensitive_output_1027=27 non_retryable_request=0 unclassified=7`。
- **`file is not a database` / `db_not_database` bucket = 0**:SQLite LOWER LIKE '%file is not a database%' OR '%db_not_database%' OR '%not a database%' on failed rows 命中 0 条,符合"图谱健康真值不应受 US-001~US-003 暴露的 DB 解密链污染"的设计目标。
- **`progress_pct=100` 真值边界(AC#6)**: `progress_pct=100` 只代表 `pending=0`,**不代表** `failed=0` 也不代表图谱完全健康。当前 failed=531 ≠ 0,桶分布显示 5 类非空(config_error/auth_error/network_timeout/json_decode_error/sensitive_output_1027/unclassified),其中 `config_error=249` 是可恢复桶(recoverableGraphBuckets 收口为 config_error/network_timeout/before_request_timeout/rate_limited 4 类,占 249+97+0+0=346,占 failed 65%)。任何只看 progress_pct 判定图谱"完成"的脚本都是 false-green。
- **写操作零触发**: 本 story 只读,未执行 `graph/resume` / `graph/rebuild` / `graph/pause` / `graph/config` 等写操作,未调用模型,未触碰 cache/key/服务文件。
- **验证**: `go build ./...` rc=0 无输出 ✅;HTTP / SQLite 双路证据完全一致 ✅;`file is not a database` bucket 严格为 0 ✅。

## US-003 DB 列表 listed/queryable 区分 (2026-06-16 17:45)

- **后端**: 新增 `internal/wechatdb/datasource/dbentry/dbentry.go` 子包,定义 `DBEntry{Path,Group,Queryable,Reason}`(防 wcdb↔datasource 循环 import);`DataSource` interface 加 `GetDBsWithStatus() (map[string][]DBEntry, error)`,老 `GetDBs()` 保留薄包装,SearchAll / wechatdb.go / service.go 三处旧调用方零改动。wcdb `classifyDB()` 用 `os.Stat` 优先 + `ds.client.IsReadableSQLite`(新增 public wrapper 封装 unexported `isReadableSQLite`)+ `CanQueryDB` 三层检查,不可读时填 `file_not_found` / `stat_error` / `core_db_unreadable` 三个封闭枚举 token(每个 < 16 chars,绝不嵌 wxid/key/path)。HTTP `handleGetDBs` 改写返回 `{dbs: map[group][]path(嵌套,向后兼容), unavailable: [{group,file,reason}], core_dbs_unavailable: bool, unavailable_reason: string}`;核心组 `coreDBGroups={session,contact,message}` 任一不可读时 `core_dbs_unavailable=true`。
- **前端**: `internal/chatlog/http/static/index.htm` `loadDBList()` 解析新 shape(优先 `data.dbs` + `data.unavailable` + `data.core_dbs_unavailable`,回退旧 map 形式),**不再并发 probe `/api/v1/db/tables`**(顺手收窄 US-001 path bug 触发面)。`core_dbs_unavailable=true` 时插入 `.alert-danger` 条幅,文案 "数据库当前不可用 / <reason 中文> / 请重扫 key 或检查 wcdb cache",替换原 "加载失败: ${e.message}"。不可查询 badge `title` 带 reason 便于悬停诊断。
- **测试**: 新增 `internal/wechatdb/datasource/wcdb/datasource_test.go` 两个 focused test:`TestClassifyDBMissingFile` 锁 `file_not_found` + token 长度 < 32 + 不含 path;`TestClassifyDBStatError` 锁非 IsNotExist stat 错误时 `reason=stat_error`。两者用 nil-client 避开 cgo 依赖,跑 0.86s。
- **Privacy**: reason token 封闭枚举,focused test 锁 `len(reason) <= 32 && !strings.Contains(reason, absPath)`,把"reason 不含 key"做成可执行 invariant 而非文档承诺。
- **验证**: `go build ./...` exit 0;`go test -count=1 ./internal/wechatdb/...` 全部 ok(wcdb + wcdbapi 含 US-002 的 3 个测试);`node -e 'new Function(loadDBListString)'` 解析 OK(前端 JS 无语法错误);`curl -sS http://127.0.0.1:5030/api/v1/db`(旧 binary 5030)返回 `map[group][]file` 12 groups,旧客户端仍能解析(backward compat);`./init.sh` quick gate 通过;`make build` 新 binary `bin/chatlog` (34.7MB) 含 US-003 代码。
- **未验证**: 新 binary 真实浏览器面板未跑 — `chatlog serve` 无 `--addr` flag,新 binary 与 5030 旧 binary 不能并存,启新 binary 必须先 kill 旧服务,属系统状态变更(Codebase Pattern 35),需用户启新 binary 后再 agent-browser 验。**新 binary 上线后**,unavailable 列表会暴露 session/contact/message_0 三条 `core_db_unreadable`(与 US-001 证据一致),前端 banner "数据库当前不可用" 自动出现 — 这是 US-004 解除 blocked 后的端到端真值信号。

## US-001 只读诊断基线 (2026-06-16)

- pwd: `/Volumes/WorkSSD/Dev/chatlog_alpha` (物理路径已 `cd "$(pwd)" && pwd -P` 验证)。
- `curl -sS --max-time 8 http://127.0.0.1:5030/health` → `{"status":"ok"}` (rc=0, HTTP 200)。
- `curl -sS --max-time 15 http://127.0.0.1:5030/api/v1/db` 列出 12 个分组、合计 18 个 DB 路径(bizchat/contact/emoticon/favorite/general/hardlink/head_image/media/message/session/sns/solitaire)。**DB 总数=18**(只记数量)。
- `/api/v1/db/tables?group=...&file=...&format=json` 对核心 3 库统一返回 **HTTP 500**:
  - session/session.db → `"stat .../db_storage/session.db: no such file or directory"`
  - contact/contact.db → `"stat .../db_storage/contact.db: no such file or directory"`
  - message/message_0.db → `"stat .../db_storage/message_0.db: no such file or directory"`
  - **根因**:HTTP handler 拼 stat 路径时漏掉了 group 子目录(`db_storage/session.db` 而非 `db_storage/session/session.db`),与 `/api/v1/db` 返回的列路径不一致。**当前 US-001 阶段判定:核心 3 库 `queryable=false`、错误摘要 `stat: no such file or directory`**,不是 `file is not a database`,但同样属"列得出但查不动"假绿。
- 源文件 stat(macOS `stat -f`):
  - `db_storage/session/session.db` size=286720 mtime=`2026-06-15 21:17:22`
  - `db_storage/session/session.db-wal` size=4194304 mtime=`2026-06-15 21:17:22`
  - `db_storage/contact/contact.db` size=16158720 mtime=`2026-06-15 21:01:51`
  - `db_storage/message/message_0.db` size=10153984 mtime=`2026-06-15 21:17:22`
- `all_keys.json` 路径=`/Users/mingtian/Library/Containers/com.tencent.xinWeChat/Data/Documents/app_data/xwechat_files/wxid_qonry7vlh3vt22_d68e/all_keys.json`,size=1963 mtime=`2026-05-31 07:57:19`(存在但 mtime 老于 6-15 的 db)。
- `~/.chatlog/wcdb_cache/*.db` count=17,oldest mtime=`2026-06-16 16:53:50`,newest=`2026-06-16 17:03:19`(11 分钟窗内全部刷新)。`sqlite3 -readonly` 探测全部 17 个 `sqlite_master` 失败:17/17 返回 `Error: in prepare, file is not a database (26)`。样例 `170fb82914dc74888c50bb22f0798c60.db`:前 16 字节 = `SQLite format 3\0`,size=10153984,sqlite_master 报 'file is not a database'。**坏 cache 实锤**,与 Codebase Pattern 命中"header 是 'SQLite format 3' 但 sqlite_master 报 'file is not a database'"。
- 库 / 图谱并发检查:`curl /api/v1/graph/status?format=json` 成功,running=false paused=false,processed=21587 failed=531 pending=0,failed_buckets: `auth_error=141 config_error=249 json_decode_error=10 network_timeout=97 sensitive_output_1027=27 unclassified=7`,`last_error=null`,`progress_pct=100`。**注:`progress_pct=100` 只表示 pending=0,failed 仍有 531,健康真值在 failed_buckets 桶分布上**,符合 US-006 待办。
- 隐私边界:全程只验证 path/mtime/size/HTTP status/error 摘要/failed_buckets 计数,**未打印** any_keys.json 内容、真实聊天内容、wcdb_cache 内容。
- US-001 收口(只读):已记录上述事实,**未触动** any DB / cache / key 文件,**未调用**模型,**未重启**服务,符合 AC#7 只读诊断约束。

## Current State (历史)

- Active feature: `db-runtime-graph-truth-harness-2026-06-12` (status: planned). New PRD converted via /ralph from `tasks/prd-db-runtime-graph-truth-harness.md`: 恢复 WeChat DB 查询链路(核心库 session/contact/message_0 可 /api/v1/db/tables 查询)、后端在 os.Rename 前用 isReadableSQLite 拒绝不可读解密产物、DB 列表区分 listed/queryable、强化 graph 健康真值(progress_pct=100 非完成态)、把排障真值链写回 skills/chatlog-http-cli。`scripts/ralph/prd.json` branch `ralph/db-runtime-graph-truth-harness`，10 stories，repair 验证通过。
  - **US-004/005/010 标 blocked**（用户决策，2026-06-12）: US-004 是破坏性运行态操作(强制重扫 WeChat key / 清坏 wcdb cache / 重启服务)，PRD Open Questions 未决，需用户 same-turn 授权后手动/本会话执行，Ralph 自主循环+MiniMax 弱模型不得自动执行；US-005(前端 DB 恢复闭环)、US-010(端到端验收)依赖 US-004。
  - **Ralph 可自主执行 7 个**: US-001 只读诊断、US-002 后端拒坏 cache、US-003 DB 状态区分、US-006 graph 真值链、US-007 digest 不扰队列、US-008 skill 写回、US-009 state 记录。
  - 真值三分法已记入 `scripts/ralph/progress.txt` Codebase Patterns: 可列出≠可查询；`isReadableSQLite` 读 `sqlite_master` 是唯一真值；坏 cache(header 对但 sqlite_master 报 'file is not a database')必须在写 outPath 前被拒。
  - 前置归档: 上个 PRD `ralph/auto-merge-branch`(4/4 完成)→ `archive/2026-06-12-auto-merge-branch/`；feature `ralph-auto-merge-branch-2026-06-12` 标 done。
- Next: run Ralph loop or manual iteration starting at US-001; US-004 需用户授权才能解除 blocked。
- ——以下为 2026-06-12 早些时候状态——

- Active feature: `ralph-auto-merge-branch-2026-06-12` (status: in-progress). New PRD converted via /ralph skill from user requirement "最后自动合并代码提交分支": ralph.py 启动时按 prd.json branchName 创建/切换工作分支，全部 story 通过(无 blocked)后自动 --no-ff 合并回 base 分支；blocked 跳过、冲突 abort、绝不 push、RALPH_AUTO_MERGE 可关。Source PRD `tasks/prd-ralph-auto-merge-branch.md`; `scripts/ralph/prd.json` 4 stories (ensure_work_branch → auto_merge_branch → sandbox 回归脚本闭环 → 文档边界同步)。**2026-06-12 20:05 状态**: US-001 ✅ passes=true (2026-06-12 17:20 evidence)；US-002 ✅ passes=true (2026-06-12 20:05 evidence: 4 条沙箱路径 clean merge / blocked skip / RALPH_AUTO_MERGE=0 / conflict abort / bootstrap placeholder 全过, `python3 -m py_compile` + `--check` exit 0 + harness 80/80)；US-003 仍 passes=false, notes 留证 `test_branch_merge.sh` 第 1 次跑 exit 1 (sys.argv 位置参数被 ralph.py 模块顶层读为 AGENT 名触发 sys.exit(2), 修复方向已记在 notes); US-004 待跑 (CLAUDE.md/VALIDATOR.md/progress.txt 文档同步)。关键设计约束: 本 PRD 改 ralph.py 自身, 验证全走 mktemp 沙箱 git 仓库 + --check, 绝不在真实仓库做分支写操作; 不修改已 dirty 的根 AGENTS.md; US-002 验证脚本一次性放 `/tmp/verify_us002_sandbox.sh` 不入仓, US-003 闭环脚本 `scripts/ralph/test_branch_merge.sh` 已存在 (startup-dirty 状态, US-003 story 收口时复用)。
- Next: implement **US-003** (test_branch_merge.sh 闭环) 修复 notes 中 `python3 - "<branch>"` 触发 sys.exit(2) 的根因, 验证 `ALL BRANCH MERGE TESTS PASSED` 输出。US-004 等 US-003 完成后接。
- ——以下为 2026-06-12 早些时候（graph-knowledge-digest 收尾）状态——

- Active feature: `graph-knowledge-digest-2026-06-10` (status: **done**). PRD `scripts/ralph/prd.json` 6/6 stories `passes=true` (US-001..US-004 implemented 2026-06-11 in-session; US-005 implemented 2026-06-12; US-006 closed loop executed externally 2026-06-11 21:23 and independently re-verified 2026-06-12 16:44).
- What changed (uncommitted — git log still ends at HA-005):
  - New: `internal/chatlog/temporalgraph/digest.go`, `digest_markdown.go` + tests (Digest read-only aggregation, Markdown renderer, summary w/ mock-tested graceful degradation, ChatInvoker falls back to Manager's semantic client when not injected).
  - Modified: `internal/chatlog/http/graph.go` (handleGraphDigest, summary param passthrough, metadata-only JSON), `internal/chatlog/http/route.go` (POST /api/v1/graph/digest), `cmd/chatlog/cmd_report.go` (`report graph` subcommand: --days/--summary/--base-url, HTTP-only, prints metadata only).
  - New doc: `docs/graph-digest.md` (usage, params, idempotent overwrite, quota/privacy boundary).
- Verification Evidence (2026-06-12):
  - `go build ./...` exit 0; `go test ./internal/chatlog/temporalgraph ./internal/chatlog/semantic ./internal/chatlog/dailyreport` all ok; `node scripts/check-root-harness.mjs` 80/80 PASS.
  - `go run . report graph --help` shows --days(7)/--summary(false)/--base-url(http://127.0.0.1:5030); unreachable base-url → error with endpoint + `chatlog serve` hint, exit 1.
  - Live closed loop on 127.0.0.1:5030: `go run . report graph --days 7` exit 0 → `reports/graph-digest-2026-06-05_2026-06-12.md` (3932 bytes, mtime current run, `grep -c '^## '` = 7, frontmatter has window_start/window_end). Second run: idempotent overwrite, exit 0, file count 1.
  - Queue untouched: failed 267 before == 267 after, pending 0. counts within status totals (entity 20≤26751, event 50≤18133, fact ≤26954, relation ≤28003). `git status --porcelain` has no `graph-digest-*.md` report artifacts (`?? docs/graph-digest.md` is the intentional new doc; `?? reports` is the pre-existing symlink line).
- Not Verified (2026-06-12): real `summary=true` model call (quota/privacy-sensitive; mock-tested in US-004; user may run once and check only summary_used + file size). `./init.sh --full` not run (focused gates above cover the change surface).
- Risks: digest code is uncommitted working-tree state mixed with pre-existing dirty files — committing needs care to exclude `reports/`, `openai_prmpt.md`, backups, and unrelated dirty files. Service binary was rebuilt and restarted 2026-06-12 to pick up the digest route.
- PRD lifecycle closed (2026-06-12): completed run archived to `archive/2026-06-12-graph-knowledge-digest/`; `scripts/ralph/prd.json` reset to bootstrap placeholder; `scripts/ralph/progress.txt` reset keeping Codebase Patterns + digest learnings (harness 80/80 after reset). Note: `scripts/ralph/`, `archive/`, `tasks/`, `.agents/`, `.claude/` have never been git-tracked — they stay local by repo convention.
- Next: nothing pending in this PRD. Digest work committed on user request (scoped to digest code/doc + root state files). Optional: one real `summary=true` run (quota-sensitive).
- ——以下为 2026-06-10 状态——
- Active feature: `graph-knowledge-digest-2026-06-10` (status: planned). Previous feature `temporal-graph-reprocess-failed-2026-06-04` moved to `monitoring` — its PRD run finished 13/14 (`US-005` blocked, documented) and the background graph worker keeps draining the queue.
- 2026-06-10 PRD work (no product code changed):
  - New PRD `tasks/prd-graph-knowledge-digest.md`: temporal graph personal knowledge digest — windowed aggregation → Obsidian-friendly Markdown (`reports/graph-digest-<start>_<end>.md`), default zero model calls, `summary=true` makes at most 1 role-neutral Chat call with fallback. User confirmed identity: traditional telecom ops engineer; do NOT embed the `openai_prmpt.md` "AI blogger" persona in prompts.
  - Previous Ralph run archived to `archive/2026-06-10-temporal-graph-failed-reprocess-parallelism/` (prd.json + progress.txt). `scripts/ralph/progress.txt` reset keeping `## Codebase Patterns`.
  - New `scripts/ralph/prd.json`: branch `ralph/graph-knowledge-digest`, 6 stories (Digest aggregation → renderer → HTTP endpoint → optional summary → CLI → closed loop). Validated via `.claude/skills/ralph/scripts/repair_prd_json.py` (exit 0).
  - 2026-06-10 /plan-eng-review (5 findings, all resolved, prd.json re-validated): US-003 output path = service CWD `reports/` with JSON absolute `path` as verification source (intentional fork from `resolveDailyReportOutputDir` workDir sandbox); US-001 entity counts = in-window tally from `graph_events.actors_json/targets_json` (graph_evidence has NO entity rows — store.go only writes relation/event/fact evidence); US-004 summary logic lives in temporalgraph w/ provider injection (http pkg has zero test infra); US-003 write-failure → HTTP 500 (cron silent-failure guard, was the one critical gap); US-005 adds `docs/graph-digest.md`. New `TODOS.md` created with graph-store WAL+busy_timeout item (do after queue drains, backup db first).
  - Discovery: `reports` is a symlink to `/Volumes/WorkSSD/Dev/openclaw_mz/knowledge/raw/微信每日聊天记录`; `.gitignore` `reports/` does not match the symlink, so `?? reports` is pre-existing untracked state. US-006 criteria adjusted to assert "no graph-digest-*.md in git status" instead of "reports/ clean".
- Not Verified (2026-06-10): no `./init.sh` run (docs/PRD/state files only, no Go changes); live `graph/status` not re-checked this session.
- Next: run the Ralph loop (`scripts/ralph/ralph.py`) starting at US-001, or implement stories manually in priority order.
- ——以下为 2026-06-07 及更早状态——
- Previous active feature: `temporal-graph-reprocess-failed-2026-06-04`.
- Subsystem: Temporal graph / semantic Chat provider / runtime graph worker.
- Status: running.
- Active PRD: `scripts/ralph/prd.json` now carries 14 stories (US-001..US-007 + HA-001..HA-007). US-001..US-004 `passes=true`, US-005 `blocked=true` (10min 窗口内 failed/pending 三连零不可达), US-006/US-007 marked superseded (see notes), HA-001..HA-007 pending. The next executable item is **HA-001** (priority=8, runtime Chat readiness / self-heal).
- Graph service is online at `127.0.0.1:5030`.
- Failed temporal graph sources were requeued. The worker is currently processing the queue with graph extraction `workers=5`.
- The `chatlog-alpha` tmux service was restarted with `MINIMAX_API_KEYS` explicitly injected; the live process environment reports 5 MiniMax keys without exposing secret values.
- Runtime hardening added: graph extraction/verification decode failures retry up to 3 times, and each graph source has a 10 minute total processing timeout to avoid worker starvation.
- MiniMax key acquisition hardening added: lease waits now derive from the request context with a bounded timeout/minimum floor in `acquireMiniMaxLeaseContext`, then return explicit `before request` errors when acquisition times out.
- Prompt hardening added: temporal graph extraction payload now removes raw `talker_id` / `sender_id`, sanitizes speaker names, limits participants/entity hints, trims context to the necessary local window, and truncates long content before sending to the Chat provider.
- MiniMax sensitive guard added: `input new_sensitive (1026)` and `output new_sensitive (1027)` are treated as non-retryable errors, so the client does not rotate through additional API keys for content safety blocks.
- Runtime server hardening added: hidden `chatlog serve --config-dir` starts the HTTP service from the service config for tmux, and service config logging no longer prints raw config structs.
- 2026-06-05 20:41 recovery: graph status had entered a half-closed state (`last_error=sql: database is closed`) while SQLite still contained `done=4951`, `failed=62`, `pending=12047`, `processing=3`. The `chatlog-alpha` tmux pane was respawned with `./bin/chatlog serve --config-dir .cache/daily-report-config`, restoring the graph manager.
- Latest live truth after recovery and quick gate: `running=true`, `source_count=17355`, `processed=4980`, `processing=1`, `pending=12357`, `failed=17`, `workers=5`, `enqueue_workers=4`, `last_error` empty. Runtime process still sees 5 MiniMax keys. Current semantic runtime config after service-config restart is `chat_provider=mmx`, `chat_model=MiniMax-M2.7`, `chat_max_tokens=4096`, `chat_temperature=0.3`.
- 2026-06-05 21:09 persistent M3 switch completed: graph was paused, `.cache/daily-report-config/chatlog-server.json` was backed up to `.cache/daily-report-config/chatlog-server.json.bak-20260605_210932`, the service config and current HTTP runtime were both set to `chat_provider=mmx`, `chat_model=MiniMax-M3`, `chat_max_tokens=8192`, `chat_temperature=0.2`, and graph was resumed.
- Latest live truth after M3 switch: `running=true`, `source_count=17355`, `processed=5210`, `processing=19`, `pending=12107`, `failed=19`, `workers=5`, `enqueue_workers=4`, `last_error` empty. Runtime process still sees 5 MiniMax keys. The 60 second post-resume monitor did not increase `failed`; `processing=19` is the claimed batch size, not the worker goroutine count.
- Side task completed: `middleware-incident-chain-report-2026-06-04` produced
  `reports/middleware-incident-chain-2026-05-31_06-03.html` from the 4872 already-processed
  graph events/facts/relations. The re-process worker was NOT paused for this work.

## What Changed

- Runtime config: semantic Chat provider was changed to `mmx` with `chat_model=MiniMax-M3`, `chat_max_tokens=8192`, and `chat_temperature=0.2`.
  - Why: current config was `glm` without an API key, so graph retry could not actually process failed sources.
- Runtime action: `POST /api/v1/graph/resume?format=json`.
  - Why: requeued recoverable `chat model is not configured` failures.
- Runtime action: `POST /api/v1/graph/rebuild?format=json` with `reset=false`.
  - Why: re-enqueue remaining failed sources without clearing the existing graph.
- Runtime config: `POST /api/v1/graph/config?format=json` with `workers=5`, `enqueue_workers=4`.
  - Why: use the 5 configured MiniMax API keys for graph extraction concurrency.
- Runtime action: injected `MINIMAX_API_KEYS` into the `chatlog-alpha` tmux environment and restarted `bin/chatlog`.
  - Why: tmux respawn inherits tmux session environment; explicit injection ensures the service process sees all 5 keys, not only the single-key `~/.mmx/config.json` fallback.
- Runtime recovery: respawned `chatlog-alpha` from the half-closed graph DB state using `./bin/chatlog serve --config-dir .cache/daily-report-config`, then ran `POST /api/v1/graph/resume?format=json`.
  - Why: HTTP health was still OK, but graph status reported `source_count=0` and `last_error=sql: database is closed`; readonly SQLite showed the real queue was not empty and `45` failed rows were recoverable `context deadline exceeded`.
- Runtime config: persisted MiniMax-M3 into `.cache/daily-report-config/chatlog-server.json` and applied the same config through `POST /api/v1/semantic/config?format=json`.
  - Why: the previous service-config restart loaded `MiniMax-M2.7`; this makes `MiniMax-M3` survive the next `chatlog serve --config-dir .cache/daily-report-config` restart.
- File: `internal/chatlog/temporalgraph/manager.go`
  - Why: retry malformed graph JSON responses before failing a source, and cap per-source processing at 10 minutes so slow upstream calls cannot occupy workers indefinitely.
- File: `internal/chatlog/temporalgraph/helpers.go`
  - Why: redact/crop temporal graph prompt payloads before model calls, including raw IDs, participant lists, entity hints, metadata, and context windows.
- File: `internal/chatlog/semantic/client.go`
  - Why: mark MiniMax 1026/1027 sensitive errors as non-retryable; add bounded lease-context helper so key acquisition aligns with parent deadlines and avoids unbounded before-request stalls.
- File: `cmd/chatlog/cmd_serve.go`
  - Why: provide a hidden runtime command for tmux to launch the HTTP service directly from the saved service config instead of falling into the TUI path.
- File: `internal/chatlog/manager.go`
  - Why: avoid logging raw service config structs during server startup.
- File: `feature_list.json`
  - Why: records this runtime reprocess task, evidence, and next monitoring path.
- File: `progress.md`
  - Why: records current runtime truth and quota/privacy boundaries.
- File: `session-handoff.md`
  - Why: leaves a restartable monitoring path.
- File: `internal/chatlog/temporalgraph/manager.go`
  - Why: add `before request` token to recoverable timeout bucket and keep per-source processing bounded to 10 minutes.

## Verification Evidence

- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/health'`
  - Result: PASS, returned `{"status":"ok"}`.
- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/graph/status?format=json'`
  - Result: PASS before requeue, `running=false`, `pending=0`, `processed=4849`, `failed=1814`.
- Command: readonly SQLite failed bucket query against `graph_source_records`
  - Result: PASS, found `1772` failures with `chat model is not configured`, plus decode/422 style failures, without printing private chat content.
- Command: `POST /api/v1/semantic/config?format=json`
  - Result: PASS, set `chat_provider=mmx`, `chat_model=MiniMax-M3`, `chat_max_tokens=8192`, `chat_temperature=0.2`.
- Command: `POST /api/v1/graph/resume?format=json`
  - Result: PASS, `pending=1772`, `failed=42`.
- Command: `POST /api/v1/graph/rebuild?format=json` with `reset=false`
  - Result: PASS, accepted; follow-up status showed `enqueue_running=true`, `source_count=6964`, `pending≈2075`, `processing=8`, `failed=41`.
- Command: latest status poll after harness check
  - Result: PASS, `running=true`, `enqueue_running=true`, `source_count=9891`, `pending=5001`, `processing=8`, `processed=4850`, `failed=35`.
- Command: `POST /api/v1/graph/config?format=json` with `{"workers":5,"enqueue_workers":4}`
  - Result: PASS, HTTP config and SQLite `graph_meta` both reported `workers=5`, `enqueue_workers=4`.
- Command: process environment key-count check after tmux restart
  - Result: PASS, live `bin/chatlog` process had `pid_env_key_count=5`; secret values were not printed.
- Command: `go test ./internal/chatlog/dailyreport ./internal/chatlog/semantic ./internal/chatlog/temporalgraph`
  - Result: PASS after graph decode retry and per-source timeout hardening.
- Command: runtime monitor after rebuild/restart/resume
  - Result: PASS, `running=true`, `workers=5`, `processed=4892`, `processing=9`, `failed=17`, `pending=12145`, `last_error` empty. Recoverable timeout/config failures were requeued; `failed` dropped from `60` to `17`.
- Command: temporal graph prompt redaction/cropping test
  - Result: PASS, source prompt payload omits `talker_id` / `sender_id`, truncates long content, limits context to 2 before + target + 1 after, caps participants at 24, and filters raw IDs from participant/entity/metadata fields.
- Command: MiniMax sensitive error retry test
  - Result: PASS, `output new_sensitive (1027)` returns after one HTTP call with two keys configured, proving key rotation is skipped for sensitive blocks. `input new_sensitive (1026)` is covered by the same non-retryable matcher.
- Command: `POST /api/v1/graph/resume?format=json` after prompt/sensitive hardening
  - Result: PASS, failed count dropped from `40` to `17`; readonly SQLite counts showed `timeout_or_config=0`, `sensitive_1026=1`, `sensitive_1027=4`.
- Command: runtime status after 20 second monitor
  - Result: PASS, `running=true`, `workers=5`, `enqueue_workers=4`, `processed=4911`, `processing=1`, `failed=17`, `pending=12134`, `last_error` empty.
- Command: `jq empty feature_list.json && ./init.sh`
  - Result: PASS, `feature_list.json` parsed, root harness check `77/77`, harness skill check `29/29`, daily report help, and HTTP endpoint list all passed.
- Command: `go test ./internal/chatlog/semantic ./internal/chatlog/temporalgraph`
  - Result: PASS, acquire-context helper and recoverable timeout-bucket update compiled and passed tests.
- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/graph/status?format=json'` after lease-context hardening
  - Result: PASS, `running=true`, `source_count=17355`, `processed=5225`, `pending=12087`, `processing=17`, `failed=26`, `workers=5`, `enqueue_workers=4`, `last_error` empty.
- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/graph/status?format=json'` during 2026-06-05 20:37 incident check
  - Result: PASS but unhealthy graph manager state: service responded, `running=false`, `source_count=0`, all queue counters `0`, `workers=5`, `enqueue_workers=4`, `last_error=sql: database is closed`.
- Command: readonly SQLite queue and failed-bucket queries during 2026-06-05 20:37 incident check
  - Result: PASS, real queue was `done=4951`, `failed=62`, `pending=12047`, `processing=3`; failed buckets were `context_deadline_exceeded=45`, `json_decode_or_format=12`, `sensitive_1027=4`, `sensitive_1026=1`.
- Command: tmux respawn plus `POST /api/v1/graph/resume?format=json`
  - Result: PASS, service restarted on `127.0.0.1:5030`, live process reported `pid_env_key_count=5`, recoverable timeout failures were requeued, and failed count dropped from `62` to `17`.
- Command: 30 second runtime monitor plus final status poll after recovery
  - Result: PASS, `failed` stayed `17`, timeout bucket cleared, processed advanced, final status after quick gate was `running=true`, `source_count=17355`, `processed=4980`, `processing=1`, `pending=12357`, `failed=17`, `workers=5`, `enqueue_workers=4`, `last_error` empty.
- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/semantic/config?format=json'`
  - Result: PASS, current runtime semantic config after service-config restart is `chat_provider=mmx`, `chat_model=MiniMax-M2.7`, `chat_max_tokens=4096`, `chat_temperature=0.3`, `has_api_key=true`.
- Command: `POST /api/v1/graph/pause?format=json` followed by status polling
  - Result: PASS, graph paused and current batch drained from `processing=7` to `processing=0`; switch window reached with `running=false`, `paused=true`, `processed=5209`, `pending=12127`, `failed=19`.
- Command: backup and `jq` update for `.cache/daily-report-config/chatlog-server.json`
  - Result: PASS, backup written to `.cache/daily-report-config/chatlog-server.json.bak-20260605_210932`; persisted config now reads `chat_provider=mmx`, `chat_model=MiniMax-M3`, `chat_max_tokens=8192`, `chat_temperature=0.2`.
- Command: `POST /api/v1/semantic/config?format=json` with API key omitted
  - Result: PASS, current HTTP runtime now reads `chat_provider=mmx`, `chat_model=MiniMax-M3`, `chat_max_tokens=8192`, `chat_temperature=0.2`, `has_api_key=true`.
- Command: `POST /api/v1/graph/resume?format=json` plus 60 second monitor
  - Result: PASS, graph resumed; latest status `running=true`, `paused=false`, `source_count=17355`, `processed=5210`, `processing=19`, `pending=12107`, `failed=19`, `workers=5`, `enqueue_workers=4`, `last_error` empty. Runtime process still reports `pid_env_key_count=5`.
- Command: readonly SQLite failed bucket query after M3 switch
  - Result: PASS, failed buckets counted without private source content: `json_decode_or_format=12`, `sensitive_1027=5`, `sensitive_1026=1`, plus one MiniMax POST error bucket; total failed stayed `19` during the switch.
- Command: `jq empty feature_list.json && ./init.sh`
  - Result: PASS, feature list parsed and quick gate passed: root harness `80/80`, repo-local skill harness `29/29`, daily report help, and HTTP endpoint list.
- Command (side task): `curl -sS 'http://127.0.0.1:5030/api/v1/graph/timeline?keyword=CRM&window=90d&limit=60&format=json'`
  - Result: PASS, 60 events including 营业厅网关告警 / 月账单连接失败 / 短号查询告警 / CIP00068 失败 / 跨域跳转 / 磐基-CRM 虚机延迟 / 145 前台故障.
- Command (side task): `curl -sS 'http://127.0.0.1:5030/api/v1/graph/timeline?keyword=跨域&window=90d&format=json'`
  - Result: PASS, 5 items, contains 跨域跳转问题反馈 / 域名跳转跨域问题排查 / relation "跨域不可访问".
- Command (side task): `curl -sS 'http://127.0.0.1:5030/api/v1/graph/timeline?keyword=中间件&window=90d&format=json'`
  - Result: PASS, 36 items, contains 12 台中间件服务器基础监控部署完成 (96 条规则) + 中间件巡检报告分享.
- File (side task): `reports/middleware-incident-chain-2026-05-31_06-03.html`
  - Result: PASS, ~28KB self-contained HTML, sections: summary / timeline / root / actions / maintain / data, all source claims carry `source_label` provenance.

## Not Verified

- Command or behavior: wait for the full graph queue to finish.
  - Reason: current processing is model-backed and may take a long time.
- Command or behavior: inspect private failed source content.
  - Reason: not needed for requeue and would expose private chat-derived text.
- Command or behavior: wait long enough to prove the full 12k+ pending queue drains under 5 workers.
  - Reason: current processing is model-backed and may take a long time; only a short runtime stability window was verified.
- Side task: full message-level search via `/api/v1/search`.
  - Reason: timed out > 8s while worker is under load; relied on graph events/facts/relations with `source_label` provenance instead.

## Blockers

- None for requeueing failed sources.

## Risks

- MiniMax-M3 produced at least one truncated JSON response with `finish_reason=length`; max tokens were raised from `4096` to `8192`, but continued monitoring is required.
- If MiniMax-M3 keeps failing structured extraction after the 3-attempt decode retry, bucket the failures first; then consider switching graph extraction back to `MiniMax-M2.7` or tightening the prompt/parser further.
- The worker is consuming MiniMax quota with up to 5 concurrent graph extraction sources while `running=true`.
- Upstream MiniMax responses are slow; the 10 minute per-source timeout prevents indefinite worker starvation but may still turn very slow requests into recoverable timeout failures.
- The remaining `17` failed rows include non-retryable sensitive blocks and decode/raw-output failures; do not blindly resume them without first bucketing and deciding whether to regenerate sanitized prompts or adjust parsing.
- MiniMax-M3 is now persisted in `.cache/daily-report-config/chatlog-server.json` and applied to the live HTTP runtime. If a future restart shows `MiniMax-M2.7`, compare against backup `.cache/daily-report-config/chatlog-server.json.bak-20260605_210932` and the current ignored service config.
- If `minimax chat failed before request: context deadline exceeded` remains dominant after this change, downgrade concurrency to `workers=3` temporarily and use `requeue` windowed resume to reduce key contention before scaling up again.

## Next

Recommended Next Step: monitor `curl -sS 'http://127.0.0.1:5030/api/v1/graph/status?format=json'`. Current latest status is `running=true`, `source_count=17355`, `pending=12087`, `processing=17`, `processed=5225`, `failed=26`, `workers=5`, `enqueue_workers=4`, `last_error` empty, and live semantic config is `MiniMax-M3`/`8192`/`0.2`. If `failed` rises under M3, run a readonly SQLite error-bucket query without printing source content, and first watch `before request` + `context deadline exceeded`占比；如果超时占比长期偏高，先降并发再考虑 prompt/parser 调整或回退到备份的 M2.7 配置。

## Side Task: Ralph AI Coding Automation Adapter 2026-06-05

- Added `.agents/`, `.claude/`, and `scripts/ralph/` assets from the AI coding automation template, excluding `.DS_Store`, `__pycache__`, `*.pyc`, and local cache files.
- Added `AGENTS.md` Ralph / PRD automation rules for `chatlog_alpha` as a Go local-tool product, not a generic Web/SaaS/content project.
- Updated `scripts/ralph/CLAUDE.md` and `scripts/ralph/VALIDATOR.md` so developer agents implement one story, write evidence, avoid direct commits, and respect privacy/quota boundaries.
- Updated `scripts/ralph/ralph.py` so automatic commit happens only after Validator success, excludes startup-dirty paths, and skips private/cache outputs such as `reports/`, `.cache/`, `logs/`, `outputs/`, and `.env*`.
- Added bootstrap `scripts/ralph/prd.json` and `scripts/ralph/progress.txt`; replace the placeholder PRD before running real product iterations.

### Ralph Verification Evidence

- Command: `find .agents .claude scripts/ralph -name '.DS_Store' -o -name '*.pyc' -o -name '__pycache__'`
  - Result: PASS, no copied or generated cache/system files remained.
- Command: `PYTHONDONTWRITEBYTECODE=1 python3 -B scripts/ralph/ralph.py --check`
  - Result: PASS, Ralph installation check passed.
- Command: `node scripts/check-root-harness.mjs`
  - Result: PASS, root harness check `77/77`.
- Command: `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs`
  - Result: PASS, repo-local harness skill check `29/29`.
- Command: `./init.sh`
  - Result: PASS, quick gate completed; root harness `77/77`, skill harness `29/29`, daily report help, and HTTP endpoint list passed.

### Ralph Not Verified

- Full autonomous Ralph loop was not run.
  - Reason: current `scripts/ralph/prd.json` is an intentional bootstrap placeholder; running the loop would either exit immediately or spawn an unnecessary agent. Use a real PRD-derived story list first.
- No commit was made.
  - Reason: this turn left existing unrelated dirty runtime files untouched and did not auto-commit.

## Side Task: Global AGENTS Rules Refresh 2026-06-05

- Used `.agents/skills/source-command-create-rules/SKILL.md` to refresh root `AGENTS.md` from the current repository shape.
- Expanded `AGENTS.md` with project overview, technology stack, verification commands, structure, code patterns, key files, scope rules, Ralph automation, Definition of Done, and End of Session.
- Preserved harness-required strings such as `Startup Workflow`, `Verification Commands`, `Definition of Done`, `feature_list.json`, `One feature at a time`, `Stay in scope`, `Ralph / PRD Automation`, and `Developer agents must not commit directly`.
- Kept this as a side task; the active temporal graph runtime feature and its monitoring path were not changed.

### AGENTS Rules Verification Evidence

- Command: `node scripts/check-root-harness.mjs`
  - Result: PASS, root harness check `77/77`.
- Command: `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs`
  - Result: PASS, repo-local harness skill check `29/29`.
- Command: `./init.sh`
  - Result: PASS, quick gate completed; root harness `77/77`, skill harness `29/29`, daily report help, and HTTP endpoint list passed.
- Command: `create-rules refresh after project-local skills conversion`
  - Result: PASS, `AGENTS.md` now documents `.agents/skills/` project-local entries including `prime`, `plan-feature`, `create-rules`, and `source-command-create-rules`; active temporal graph runtime scope was unchanged.
- Command: `jq empty feature_list.json`
  - Result: PASS.
- Command: `node scripts/check-root-harness.mjs`
  - Result: PASS, root harness check `80/80`.
- Command: `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs`
  - Result: PASS, repo-local harness skill check `29/29`.
- Command: `./init.sh`
  - Result: PASS, quick gate completed; root harness `80/80`, skill harness `29/29`, daily report help, and HTTP endpoint list passed.
- Command: `2026-06-06 create-rules refresh`
  - Result: PASS, `AGENTS.md` now clarifies `scripts/ralph/prd.json` `userStories` inspection before Ralph automation, treats `reports.backup-*` as private generated artifacts, and records `archive/` / `tasks/` as scoped work artifacts.
- Command: `jq empty feature_list.json`
  - Result: PASS.
- Command: `node scripts/check-root-harness.mjs`
  - Result: PASS, root harness check `80/80`.
- Command: `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs`
  - Result: PASS, repo-local harness skill check `29/29`.
- Command: `./init.sh`
  - Result: PASS, quick gate completed; root harness `80/80`, skill harness `29/29`, daily report help, and HTTP endpoint list passed.

### AGENTS Rules Not Verified

- Full gate `./init.sh --full` was not run.
  - Reason: documentation/rules-only side task; quick harness gate was sufficient and avoids unnecessary longer build/test work in an already dirty runtime workspace.
- Runtime gate `./init.sh --runtime` was not run.
  - Reason: rules refresh did not claim live runtime behavior.
- Full gate `./init.sh --full` was not run for the create-rules refresh.
  - Reason: documentation/rules-only update; quick harness gate was sufficient.
- Runtime gate `./init.sh --runtime` was not run for the 2026-06-06 create-rules refresh.
  - Reason: rules refresh did not claim live runtime behavior.

## Side Task: Project Command Skills Localization 2026-06-05

- Converted project-only command shortcuts into local Codex-style skills under `.agents/skills/`.
- Added `.agents/skills/prime/SKILL.md` for loading project context without editing files.
- Added `.agents/skills/plan-feature/SKILL.md` for creating implementation plans without writing code.
- Added `.agents/skills/create-rules/SKILL.md` as a compatibility entry for the old `create-rules` command name while preserving `.agents/skills/source-command-create-rules/SKILL.md`.
- Kept original `.agents/commands/*.md` files in place; no files were deleted or moved.
- Updated `scripts/check-root-harness.mjs` so root harness verifies the three local command skills exist.
- No global skill directories were modified.

### Project Command Skills Verification Evidence

- Command: `find .agents/skills -maxdepth 2 -name 'SKILL.md' -print | sort`
  - Result: PASS, includes local `create-rules`, `plan-feature`, and `prime` skills.
- Command: `node scripts/check-root-harness.mjs`
  - Result: PASS, root harness check `80/80`.
- Command: `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs`
  - Result: PASS, repo-local harness skill check `29/29`.
- Command: `jq empty feature_list.json`
  - Result: PASS.

### Project Command Skills Not Verified

- Current UI skill picker refresh was not verified.
  - Reason: newly created local skills may require a new Codex turn/session or project reload before appearing in the picker.
- Global skill installation was not performed.
  - Reason: user requested current-project-only usage.

## Runtime Recovery: Temporal Graph Tail Completion 2026-06-06

- Objective: stabilize and finish the active temporal graph tail without blindly requeueing the large failed bucket.
- Starting live status: `running=true`, `workers=3`, `processed=7523`, `processing=7`, `pending=17`, `failed=13488`, `last_error="minimax chat failed before request: context deadline exceeded"`.
- Safety decision: did NOT call `POST /api/v1/graph/resume?format=json` after discovering it would requeue `13473` rows with `chat model is not configured`.
- Runtime action: backed up `.cache/daily-report-config/chatlog-server.json` to `.cache/daily-report-config/chatlog-server.json.bak-20260606_155428`, then aligned the persisted service config with `MiniMax-M3`/`8192`/`0.2`.
- Runtime action: set graph workers to `2` and respawned `chatlog-alpha` as `./bin/chatlog serve --config-dir .cache/daily-report-config`; verified `/health` returned `{"status":"ok"}` and live process had `MINIMAX_API_KEYS_count=5`.
- Observation: M3 tail batch made no progress for several minutes after restart, with `last_error` clear but `processing=10`, `pending=15`, `processed=7523`.
- Runtime action: backed up `.cache/daily-report-config/chatlog-server.json` to `.cache/daily-report-config/chatlog-server.json.bak-20260606_160521-m27-tail`, switched tail processing to `MiniMax-M2.7`/`4096`/`0.2`, set `workers=1`, `enqueue_workers=1`, and respawned `chatlog-alpha`.
- Result: tail completed. Final API status: `running=false`, `paused=false`, `workers=1`, `enqueue_workers=1`, `source_count=21036`, `processed=7547`, `processing=0`, `pending=0`, `failed=13489`, `progress_pct=100`, `last_error=null`, `last_updated_at=2026-06-06T16:15:03+08:00`.
- Final readonly SQLite bucket: `done=7547`, `failed=13489`; failed buckets were `chat_model_not_configured=13473`, `decode_graph_extraction_failed=9`, `minimax_sensitive_1027=3`, `context_deadline_exceeded=2`, `before_request_timeout=1`, `other=1`.
- Current persisted tail config is intentionally `MiniMax-M2.7`/`4096`/`0.2` for stability. Restore M3 with backup `.cache/daily-report-config/chatlog-server.json.bak-20260606_155428` or reapply M3 explicitly before any quality-sensitive reprocess.

### Runtime Recovery Verification Evidence

- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/health'`
  - Result: PASS, returned `{"status":"ok"}`.
- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/graph/status?format=json'`
  - Result: PASS, final status `running=false`, `pending=0`, `processing=0`, `failed=13489`, `last_error=null`.
- Command: readonly SQLite status and failed-bucket query against `/Volumes/HDD/chatlog/wxid_qonry7vlh3vt22_d68e/.chatlog_graph/temporal_graph.db`
  - Result: PASS, counted buckets without printing private source content.
- Command: live process check through `lsof`/`ps`
  - Result: PASS, process is `./bin/chatlog serve --config-dir .cache/daily-report-config` and has `MINIMAX_API_KEYS_count=5`.

### Runtime Recovery Not Verified

- Did not requeue the `13473` `chat model is not configured` failures.
  - Reason: that would launch a large model-backed reprocess and consume substantial MiniMax quota; it needs an explicit staged plan.
- Did not inspect private failed source contents.
  - Reason: only status, counts, buckets, paths, and timestamps were needed for runtime recovery.
- Did not run `./init.sh --full`.
  - Reason: this was live runtime recovery; final evidence came from health, graph status, SQLite buckets, and process checks.

## Runtime Reprocess: Failed Sources 2026-06-06 20:22

- Objective: user explicitly requested querying today's logs and reprocessing failed temporal graph sources.
- Privacy boundary: did not print private source content, prompts, raw model output, or API keys. Evidence used status, counts, timestamps, bucketed error classes, HTTP access logs, and file metadata only.
- Today's file log: `logs/chatlog-alpha-20260606_090057.log` exists but is `0B`; tmux pane only retained HTTP access log lines, not detailed model failure logs.
- Pre-reprocess API status matched the UI: `running=false`, `pending=0`, `processing=0`, `processed=7584`, `failed=13489`, `source_count=21073`, `progress_pct=100`, `last_updated_at=2026-06-06T18:12:31+08:00`.
- Time-window check using `2026-06-06 00:00:00 +0800`: today had `done=6501`, `failed=13479`, updated from `2026-06-06 00:15:28` to `2026-06-06 18:12:31`.
- Today's failed buckets before reprocess: `chat_model_not_configured=13473`, `context_deadline_exceeded=3`, `sensitive_1027=2`, `json_decode_or_format=1`.
- Runtime config before reprocess: HTTP semantic config reported `chat_provider=mmx`, `chat_model=MiniMax-M2.7`, `chat_max_tokens=4096`, `chat_temperature=0.2`, `has_api_key=true`; graph config was `workers=1`, `enqueue_workers=1`.
- Runtime action: ran `POST /api/v1/graph/resume?format=json`. The code path requeues recoverable config and timeout failures, without `reset=true` and without clearing existing graph data.
- Result immediately after resume: `running=true`, `pending=13466`, `processing=10`, `processed=7584`, `failed=13`, `started_at=2026-06-06T20:22:06+08:00`.
- Short monitor result: processed advanced to `7586`, `processing=8`, `failed=13`; graph counts advanced to `entity_count=11672`, `relation_count=10404`, `event_count=6350`, `fact_count=9942`.
- Final poll after state-file update: `running=true`, `pending=13466`, `processing=2`, `processed=7592`, `failed=13`, `last_updated_at=2026-06-06T20:26:05+08:00`.
- Final poll after `./init.sh`: `running=true`, `pending=13456`, `processing=9`, `processed=7595`, `failed=13`, `last_updated_at=2026-06-06T20:27:04+08:00`.
- Remaining failed buckets after reprocess: `json_decode_or_format=10`, `sensitive_1027=3`. `chat_model_not_configured` is cleared from the failed bucket.

### Runtime Reprocess Verification Evidence

- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/health'`
  - Result: PASS, returned `{"status":"ok"}`.
- Command: `curl -sS --max-time 12 'http://127.0.0.1:5030/api/v1/graph/status?format=json'`
  - Result: PASS, pre-reprocess status matched the user-provided idle failed state.
- Command: readonly SQLite bucket and today(+0800) timestamp queries against `/Volumes/HDD/chatlog/wxid_qonry7vlh3vt22_d68e/.chatlog_graph/temporal_graph.db`
  - Result: PASS, counted failures by class and day without printing private content.
- Command: `POST /api/v1/graph/resume?format=json`
  - Result: PASS, recoverable failures moved from `failed` to `pending`, leaving only 13 non-requeued failures.
- Command: 60 second status monitor
  - Result: PASS, processing resumed and advanced from `processed=7584` to `processed=7586`; `failed` stayed `13`.
- Command: `jq empty feature_list.json && ./init.sh`
  - Result: PASS, `feature_list.json` parsed, root harness check `80/80`, repo-local skill harness check `29/29`, daily report help, and HTTP endpoint list passed.

### Runtime Reprocess Not Verified

- Full drain of the `13466` pending reprocess queue was not waited.
  - Reason: this is model-backed and may take a long time under `workers=1`.
- Detailed model failure log contents were not available from `logs/chatlog-alpha-20260606_090057.log`.
  - Reason: the file is empty; tmux only retained access logs.
- Remaining `json_decode_or_format` and `sensitive_1027` failed rows were not requeued manually.
  - Reason: these are not the safe recoverable config/timeout bucket; requeueing them blindly risks repeated parser/safety failures.

### Runtime Reprocess Next

- Monitor:
  `curl -sS 'http://127.0.0.1:5030/api/v1/graph/status?format=json' | jq '{running,processed,processing,pending,failed,last_error}'`
- If `failed` rises, run a readonly bucket query first and keep `chat_model_not_configured`, timeout, decode, and sensitive buckets separate.
- If throughput is too slow and failed remains stable, consider raising `workers` cautiously after checking MiniMax key availability and quota risk.

## Runtime HA Recovery: Temporal Graph Config Drift 2026-06-07

- Objective: recover from `last_error="chat model is not configured"` and make the service harder to drift back into a no-key Chat provider state.
- Root cause: the live process was started as `./bin/chatlog`, so HTTP runtime config drifted to `chat_provider=glm`, `chat_model=glm-5.1`, `has_api_key=false`. Persisted service config `.cache/daily-report-config/chatlog-server.json` still had `chat_provider=mmx`, `chat_model=MiniMax-M2.7`, `chat_max_tokens=4096`, `chat_temperature=0.2`, `has_api_key=true`.
- User clarified that `~/.zshrc` config contains 5 MiniMax API keys. `zsh -lc` could read `zshrc_minimax_key_count=5`, but the tmux session initially had no `MINIMAX_API_KEYS`.
- Runtime action: injected `MINIMAX_API_KEYS` from zsh into tmux session without printing secrets, then respawned `chatlog-alpha` as `./bin/chatlog serve --config-dir .cache/daily-report-config`.
- Verification: HTTP health returned `{"status":"ok"}`; semantic config read back `chat_provider=mmx`, `chat_model=MiniMax-M2.7`, `has_api_key=true`; process env count read `pid_minimax_key_count=5`.
- Runtime action: ran `POST /api/v1/graph/resume?format=json`; failed dropped from `11079` to `17`, with `pending=11062` and `running=true`.
- Runtime action: ran `POST /api/v1/graph/config?format=json` with `workers=5`, `enqueue_workers=1`.
- Short monitor after 5-key recovery: `workers=5`, `running=true`, `pending=11037`, `processed=10017`, `failed=17`, and remaining failed buckets were `json_decode_or_format=10`, `sensitive_1027=7`.
- Final status after quick gate: `running=true`, `workers=5`, `pending=11037`, `processing=8`, `processed=10019`, `failed=17`, `last_updated_at=2026-06-07T09:01:58+08:00`.
- Added `scripts/chatlog-ha-guard.sh` as a reusable guard. It checks health, semantic config, graph status, config-failed bucket, tmux key environment, and can self-heal by restarting the correct service entry and resuming recoverable graph failures.
- Fixed guard dry-run so it never prints `MINIMAX_API_KEYS`; it reports key counts only.

### Runtime HA Verification Evidence

- Command: `zsh -lc` key-count check
  - Result: PASS, reported `zshrc_minimax_key_count=5` without printing key values.
- Command: `tmux show-environment -t chatlog-alpha MINIMAX_API_KEYS` key-count check
  - Result: PASS after injection, reported `tmux_minimax_key_count=5` without printing key values.
- Command: process environment key-count check for the listener pid
  - Result: PASS, reported `pid_minimax_key_count=5` without printing key values.
- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/semantic/config?format=json'`
  - Result: PASS, runtime config is `mmx` / `MiniMax-M2.7` / `has_api_key=true`.
- Command: `POST /api/v1/graph/resume?format=json`
  - Result: PASS, recoverable config failures were requeued and `failed` dropped to `17`.
- Command: `POST /api/v1/graph/config?format=json` with `{"workers":5,"enqueue_workers":1}`
  - Result: PASS, status read back `workers=5`, `enqueue_workers=1`.
- Command: `bash -n scripts/chatlog-ha-guard.sh && scripts/chatlog-ha-guard.sh --dry-run && scripts/chatlog-ha-guard.sh`
  - Result: PASS, guard syntax is valid, dry-run redacts keys, and actual run was idempotent.
- Command: `jq empty feature_list.json && ./init.sh`
  - Result: PASS, `feature_list.json` parsed, root harness check `80/80`, repo-local skill harness check `29/29`, daily report help, and HTTP endpoint list passed.

### Runtime HA Not Verified

- Full drain of the `11037` pending queue was not waited.
  - Reason: model-backed graph extraction may take a long time.
- Long soak of the HA guard loop was not run.
  - Reason: the immediate incident was recovered; `--loop` can be run in tmux/launchd for continuous guarding.
- Remaining `json_decode_or_format` and `sensitive_1027` rows were not manually requeued.
  - Reason: they are not safe config/timeout recoverable failures.

### Runtime HA Next

- One-shot guard:
  `scripts/chatlog-ha-guard.sh`
- Continuous guard in tmux:
  `tmux new-session -d -s chatlog-ha 'cd /Volumes/WorkSSD/Dev/chatlog_alpha && scripts/chatlog-ha-guard.sh --loop 60 >> logs/chatlog-ha-guard.log 2>&1'`
- Monitor:
  `curl -sS 'http://127.0.0.1:5030/api/v1/graph/status?format=json' | jq '{running,workers,processed,processing,pending,failed,last_error}'`

## Side Task: PRD Merge — HA Keypool & Adaptive Routing 2026-06-07

- Source PRD: `tasks/prd-temporal-graph-ha-keypool.md` (174 行, 7 个 User Story, 10 个 Functional Requirement, 5 个 Non-Goal, 显式 Verification Plan 与 Open Questions).
- Action: 把 7 个 Story 拆 JSON 追加到 `scripts/ralph/prd.json` 的 `userStories` 数组,priority 8..14;旧 US-006 / US-007 在 `notes` 字段标记 superseded_by(HA-004 / HA-007)而不是删除,保留审计链。
- Privacy boundary: 拆 JSON 时未把任何 `sk-...` API key 或私聊原文写入 `prd.json`;HA Story 自带的脱敏要求由 AC 条款控制(状态接口只返回 `key_1` 之类 label,benchmark fixture 禁止使用真实聊天正文)。
- 备份: `/tmp/prd.backup.json` 保留入仓前 7-story 版本(8446 字节)。

### PRD Merge Verification Evidence

- Command: `jq '.userStories | length'` on `scripts/ralph/prd.json`
  - Result: PASS, `14` stories.
- Command: `jq -r '.userStories[] | .id'`
  - Result: PASS, order = US-001, US-002, US-003, US-004, US-005, US-006, US-007, HA-001..HA-007.
- Command: `jq '.userStories[] | select(.id=="US-006" or .id=="US-007") | {id, passes, blocked, notes}'`
  - Result: PASS, `passes=false, blocked=false` 保留,`notes` 写明 superseded 关系。
- Command: `jq '.userStories[7:14] | map({id, ac: (.acceptanceCriteria|length), src: (.sourcePrd!=null), sup: (.supersedes!=null)})'`
  - Result: PASS, 7 个 HA Story 均有 5 条 AC,`sourcePrd` 指向 PRD 路径,HA-004 / HA-007 标 `supersedes` 旧 story。
- Command: `./init.sh`
  - Result: PASS, quick gate "Verification Complete",root harness + skill harness + HTTP list 全过。

### PRD Merge Not Verified

- `./init.sh --full` 未跑
  - Reason: 本轮只动 `prd.json` 与 `progress.md` / `progress.txt` / `session-handoff.md` 文档,无 Go 改动,quick gate 已证明 harness 未被破坏。
- 旧 US-006 / US-007 的 `passes` 字段未置为 `true`
  - Reason: 留给 `ralph.py` 或后续人工在 HA-004 / HA-007 通过后,写 notes "completed as part of HA-xxx" 并把 `passes` 翻 true;本次只在 notes 标 superseded。

### PRD Merge Next

- 下一个 Ralph 可执行项:**HA-001**(priority=8,runtime Chat readiness observable + self-healable)。属于 runtime 改造 story,可能动到 `internal/chatlog/http/`、`internal/chatlog/semantic/` 与 `scripts/chatlog-ha-guard.sh`。
- 备选手动接管:按 `tasks/prd-temporal-graph-ha-keypool.md` 第 9 节 Verification Plan 顺序执行,验证后逐 story 把 `passes` 翻 true。
- 当前 active runtime status(latest): `running=true, source_count=21073, processed=10019, pending=11037, processing=8, failed=17, workers=5, enqueue_workers=1, last_error` 为空,5 个 MiniMax key 仍在进程内。

## Side Task: PRD Story HA-001 — Runtime Chat readiness observable + self-healable 2026-06-07

- 目标:让运维/operator 通过 HTTP 与 `scripts/chatlog-ha-guard.sh` 能在 60 秒内观察到 `chat_provider=mmx + has_api_key=true + configured_key_count=5` 并自动从 `chat model is not configured` 漂移中恢复。
- 改动:
  - `internal/chatlog/semantic/client.go`:新增 `miniMaxAPIKeyPool.Snapshot()` 与导出 `MiniMaxKeyPoolStatus()`,新增 `recordLease / recordError / redactedSignature / classifyMiniMaxErrorBucket`,把 `chatMMXRaw` 与 `analyzeMiniMaxImage` 的成功 lease 与错误桶都接进 pool,key pool 重建时计数归零。
  - `internal/chatlog/semantic/client_test.go`:新增 4 个测试覆盖 Snapshot 不泄漏、busy/idle 计数、错误桶分类、空 err 处理。
  - `internal/chatlog/http/route.go`:新增 `GET /api/v1/semantic/mmx/status?format=json`(`handleSemanticMMXStatus`);`handleSemanticConfigGet` 增加 `configured_key_count` 与 provider-aware `has_api_key`,只在 `chat_provider == mmx` 时把 env 侧 `MINIMAX_API_KEYS` 计入。
  - `scripts/chatlog-ha-guard.sh`:`load_minimax_env` 增加 `~/.chatlog-ha-keys` 与 `~/.zshenv` / `~/.zshrc` 兜底;新增 `runtime_minimax_status` 优先调用 mmx status endpoint(回退到 env 计数);`check_once` 接入 mmx status 摘要与 `mmx_key_underprovisioned` 自愈;新增 `restore_semantic_config` 自愈 POST,respawn 仅在 POST 后仍漂移时触发。
  - `scripts/ralph/prd.json`:`HA-001.passes = true`。
- 验证命令与结果(均通过):
  - `go build ./...` PASS。
  - `go test ./internal/chatlog/semantic ./internal/chatlog/temporalgraph` PASS,4 个新测试 + 所有旧测试。
  - `bash -n scripts/chatlog-ha-guard.sh` PASS,语法 OK。
  - `scripts/chatlog-ha-guard.sh --dry-run` 输出仅含状态/计数/model/bucket,`minimax_env key_count=5`(走 zshenv 兜底),无任何 `sk-` 泄漏。
  - 用 alt port 5099/5098 起临时 `bin/chatlog serve --config-dir .cache/ha001-test-config(-bad)` 验证:
    - 好配置:`/api/v1/semantic/config?format=json` → `chat_provider=mmx, has_api_key=true, configured_key_count=5`。
    - `/api/v1/semantic/mmx/status?format=json` → `configured_key_count=5, busy_key_count, idle_key_count=5, leased_request_count, retry_count, last_error_bucket, key_labels=***1111..***5555, signature=<sha256 short hash>`,response grep `sk-` 0 命中。
    - 坏配置自愈:用 `chat_provider=ollama, api_key=""` 起实例,跑 guard → 3 秒内 `WARN semantic_config_drift → ACTION restore_semantic_config → semantic provider=mmx model=MiniMax-M2.7 has_api_key=true → mmx_status configured=5`,再次 `curl` 全部满足 AC#3。
  - `./init.sh` quick gate PASS,`Verification Complete`。
- 未验证项:
  - 生产 chatlog-alpha (pid 45957) 仍是旧 binary,未重启;HA-001 的端到端 self-heal 在 alt port 的临时实例上验证。下一次生产 tmux/launchd 重启窗口把 `./bin/chatlog` 升级即可生效。
  - 临时 `bin/chatlog serve --config-dir .cache/ha001-test-config(-bad)` 在验证完成后已 kill 并删除 `.cache/ha001-test-config*` 与 `/tmp/ha001-*.log`,无残留状态。
- 后续:HA-002 复用 mmx status 端点做 key 隔离/切换;HA-003 复用 `mmx_status.retry_count / last_error_bucket` 派生自适应 worker 阈值;HA-006 / HA-007 把生产 binary 切到本次构建的版本。

## Side Task: PRD Story HA-002 — MiniMax 5-key pool status can be inspected safely 2026-06-07

- 目标:让 operator/HA guard 通过 HTTP 看到 pool-level quarantine 状态(quarantined_key_count / quarantined_labels / last_quarantined_label 等),便于判断 "key 数量 5 但实际可用的 healthy key < 5" 的情况。
- 修复 (HA-002 第 1 次验证失败 → 修复 → 第 2 次通过):
  - `internal/chatlog/http/route.go` `handleSemanticMMXStatus` 在 gin.H 响应中把 `miniMaxKeyPoolSnapshot` 已有的 6 个 quarantine 字段全部 spread:`quarantined_key_count` / `healthy_key_count` / `quarantined_labels` / `last_quarantined_label` / `last_quarantined_for` / `last_quarantined_at`。
  - snapshot 端与 pool 内部 quarantine 行为(`recordError` + `tryAcquire` 跳过 quarantined slot)已是 HA-002 第 1 次提交时实现;`TestRecordErrorQuarantinesKeyOnAuthError` / `TestAcquireSkipsQuarantinedKey` / `TestRecordErrorDoesNotQuarantineOnSensitiveOrRateOrDecode` 单元测试 3/3 PASS。
  - `scripts/ralph/prd.json` `HA-002.passes = true`, `notes = ""`, `retryCount = 0`。
- 验证 (alt port 5097,5 个 dummy `MINIMAX_API_KEYS` 启动,验证后清理):
  - `go build ./...` PASS。
  - `go test -count=1 ./internal/chatlog/semantic ./internal/chatlog/temporalgraph` PASS。
  - fresh 状态 `curl http://127.0.0.1:5097/api/v1/semantic/mmx/status?format=json` 返回 19 字段齐全,quarantine 全 0/[]/"",`configured_key_count=5`。
  - 触发 `POST /api/v1/semantic/test` 后再查询:返回 `quarantined_key_count=5, healthy_key_count=0, quarantined_labels=[key_1,key_2,key_3,key_4,key_5], last_quarantined_label=key_2, last_quarantined_for=minimax_auth_error, last_error_bucket=minimax_auth_error`,字面满足 AC#4 "状态接口显示该 key label 进入隔离或失败状态"。
  - response grep `sk-cp-` 0 命中,label 形态 `key_1..key_5`,满足 AC#2 脱敏要求。
  - `./init.sh` quick gate PASS,`Verification Complete`。
- 未验证项:
  - 生产 chatlog-alpha (pid 45957) 仍是旧 binary,未重启;HA-002 的端到端 quarantine 在 alt port 的临时实例上验证。下一次生产 tmux/launchd 重启窗口把 `./bin/chatlog` 升级即可生效。
  - 临时实例 + `.cache/ha002-test-config` 在验证完成后已 kill 并删除,无残留状态。
- 后续:HA-006 guard 可直接读 `quarantined_key_count` / `healthy_key_count` 做 "key count=5 但 healthy < 5" 告警;HA-003 复用 `quarantined_key_count` / `retry_count` 派生自适应 worker 阈值;HA-006 / HA-007 切生产 binary 后做完整端到端验证。

## Runtime Reprocess: Temporal Graph ollama_refused Recovery 2026-06-08

- Objective: reprocess failed temporal graph sources from the user-provided idle status `source_count=21144, processed=17018, failed=4126, pending=0`.
- Privacy/quota boundary: only status counts and error buckets were inspected; no chat source content, prompt payload, or API key value was printed. This reprocess consumes MiniMax Chat quota.
- Initial live status:
  - Runtime: `./bin/chatlog serve --config-dir .cache/daily-report-config` on `127.0.0.1:5030`.
  - Semantic config: `chat_provider=mmx`, `chat_model=MiniMax-M2.7`, `chat_max_tokens=4096`, `chat_temperature=0.2`, `has_api_key=true`.
  - HA guard dry-run: MiniMax env key count `5`, mmx status `configured=5`, `busy=0`, `idle=5`, historical `retry=56`, `last_error_bucket=minimax_timeout`.
- Failed bucket snapshot before requeue:
  - `ollama_refused=3951` from `2026-06-07 10:03:10` to `10:08:09`.
  - `minimax_401=141`.
  - `sensitive_1027=18`.
  - `json_decode_or_format=10`.
  - `zero_key_attempts=5`.
  - `other=1`.
- Actions:
  - Set graph config to `workers=3`, `enqueue_workers=1` to avoid restarting a large failed batch at full concurrency.
  - Existing `POST /api/v1/graph/resume?format=json` did not move rows because `ollama_refused` is unclassified by the current recoverable bucket list.
  - Canary requeued exactly 10 `ollama_refused` failed rows by setting them to `pending`. The canary moved into `processing`; 9 reached `processed` during the observation window and `failed` did not increase.
  - Requeued the remaining 3941 `ollama_refused` rows only. Did not requeue `minimax_401`, `sensitive_1027`, `json_decode_or_format`, `zero_key_attempts`, or `other`.
- Latest verification:
  - `GET /api/v1/graph/status?format=json` at `2026-06-08T07:20:24+08:00`: `running=true`, `pending=3911`, `processing=8`, `processed=17050`, `failed=175`, `workers=3`, `enqueue_workers=1`, `processing_rate_per_minute≈11.28`, `last_error=null`.
  - SQLite status: `done=17050`, `failed=175`, `pending=3911`, `processing=8`.
- Not verified:
  - Full drain of the 3911 pending requeued rows.
  - Whether `minimax_401` rows are recoverable; they were intentionally not retried.
  - JSON decode and sensitive rows were not reprocessed.
- Next:
  - Monitor: `curl -sS 'http://127.0.0.1:5030/api/v1/graph/status?format=json' | jq '{running,processed,processing,pending,failed,workers,last_error}'`.
  - When pending drains, run a readonly failed bucket query again. If `minimax_401` remains, verify MiniMax account/key authorization before any retry. Keep `sensitive_1027` failed unless an explicit safety/prompt policy decision is made.
