# Session Handoff

Last Updated: 2026-06-16

## Current Objective

- Goal: execute the active PRD `scripts/ralph/prd.json` — branch `ralph/db-runtime-graph-truth-harness`, 10 stories. **7/10 完成 passes=true**(US-001/002/003/006/007/008/009);**3/10 blocked=true** 待用户 same-turn 授权(US-004 强制重扫 WeChat key + 清坏 wcdb cache + 重启服务 / US-005 前端 DB 恢复闭环 / US-010 端到端验收,均依赖 US-004)。**0/10 可自主执行剩余**;Ralph 自主循环等待用户授权 US-004 或由用户手动接管。Feature: `db-runtime-graph-truth-harness-2026-06-12` in `feature_list.json` (status: **in_progress**)。Source PRD: `tasks/prd-db-runtime-graph-truth-harness.md`。
- **已完成的代码改动(2026-06-16)**:
  - US-001: 只读诊断(无代码改动,evidence 已写入 progress.md)。
  - US-002: `internal/wechatdb/wcdbapi/client.go` `ensureDecrypted` 在 `os.Rename(tmpPath, outPath)` 之前加 `isReadableSQLite(tmpPath)` 闸门,不可读时删 tmpPath 不更新 c.cache,错误含 actionable hint + 绝不嵌入 key。
  - US-003: 新增 `internal/wechatdb/datasource/dbentry/dbentry.go`(共享 `DBEntry` 类型);`DataSource.GetDBsWithStatus()` 接口 + wcdb `classifyDB` 三层检查(os.Stat + `Client.IsReadableSQLite` + `CanQueryDB`),reason token 封闭枚举(`file_not_found` / `stat_error` / `core_db_unreadable`);HTTP `/api/v1/db` 返回 `{dbs, unavailable, core_dbs_unavailable, unavailable_reason}`;前端 `loadDBList` 解析新 shape + 显式 banner + 不再并发 probe `/api/v1/db/tables`。
  - US-006/007/008: graph 真值链 + digest non-summary 不扰队列 + skill 写回 truth chain(references/db-truth-chain.md, graph-truth-chain.md, feedback-audit.md 更新 Known False Greens)。
  - US-009: root state 收口(`feature_list.json` evidence + status=in_progress,`progress.md` US-009 段,本 handoff 段刷新)。
- **5030 服务状态**: 仍跑**旧 binary**(`make build` 后 `bin/chatlog` 是新的,但 5030 进程未重启);旧 binary 的 `/api/v1/db` 仍返回 `map[group][]path` 旧 shape,前端 fallback 兼容路径仍正常显示列表(无 banner,因为没有 `core_dbs_unavailable` 字段)。**新 binary 需用户手动启服务后生效**(Codebase Pattern 35: 服务二进制更新后路由 404,需重启服务)。
- **Blocked stories(需用户 same-turn 授权)**:
  - US-004: 备份 `all_keys.json`(时间戳后缀) + 单步删坏 wcdb cache(一次一个) + 强制重扫 WeChat key + 重启 chatlog-alpha 服务 — 破坏性运行态操作,PRD Open Questions 未决。
  - US-005: 依赖 US-004 完成;前端 DB 面板 `core_dbs_unavailable` banner 消失,核心 3 库 tables 加载成功。
  - US-010: 依赖 US-004 完成;端到端验收(可查询核心库 + graph failed bucket 0 + digest 不扰队列 + 全部 harness check 通过)。
  - 解除 blocked 前提: 用户 same-turn 明确授权 + 仅人工/本会话执行 + 先备份 all_keys.json + 单步删 cache + 每步复验。Ralph 自主循环不得自动执行破坏性运行态操作。
- **Key constraints**: 只验证 path/mtime/size/count/status/bucket,绝不打印聊天正文/真实 API key/真实 data key;'可列出'≠'可查询'(`isReadableSQLite` 读 `sqlite_master` 是唯一真值);`progress_pct=100` 只代表 `pending=0` 不代表 `failed=0`;不盲目 requeue graph failed rows;不批量删除文件。
- **Next-session diagnostic commands(read-only)**:
  - `pwd -P` → 期望 `/Volumes/WorkSSD/Dev/chatlog_alpha`
  - `curl -sS --max-time 8 http://127.0.0.1:5030/health` → 期望 `{"status":"ok"}`(precheck,不是 DB 完成态)
  - `curl -sS --max-time 15 http://127.0.0.1:5030/api/v1/db` → 12 groups / 18 DB paths(旧 binary);新 binary + 真实 wcdb cache 时返回新 shape 含 unavailable 列表
  - `curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/graph/status?format=json' | jq '{failed, failed_buckets, last_error, progress_pct}'` → 期望 `failed=531 / last_error=null / progress_pct=100`(progress_pct 不代表完成,真值在 failed_buckets 分布)
  - `jq empty feature_list.json && node scripts/check-root-harness.mjs` → 期望 80/80 PASS
  - `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs` → 期望 29/29 PASS

## Previous Objective (ralph/auto-merge-branch, 2026-06-12, COMPLETE)

- Goal: **COMPLETE** — PRD `ralph/auto-merge-branch` 4/4 stories `passes=true`, archived to `archive/2026-06-12-auto-merge-branch/`. ralph.py 分支生命周期(ensure_work_branch/auto_merge_branch)已上线，沙箱三场景回归(`bash scripts/ralph/test_branch_merge.sh` → ALL BRANCH MERGE TESTS PASSED)通过；`ralph.py --check` exit 0；ralph.py 无 git push。Feature `ralph-auto-merge-branch-2026-06-12` 标 done。注意 scripts/ralph/ 在 git 中是 untracked(仓库惯例)，代码改动留在工作区未提交。

## Previous Objective (graph-knowledge-digest, 2026-06-12, COMPLETE)

- Goal: **COMPLETE** — PRD (graph-knowledge-digest) 6/6 stories `passes=true`, archived to `archive/2026-06-12-graph-knowledge-digest/`, committed as 8c556de2 (HA leftovers fix) + fbaffcc2 (digest feature). Feature `graph-knowledge-digest-2026-06-10` marked `done` in `feature_list.json` with evidence; details in `progress.md` (2026-06-12 section) and `scripts/ralph/progress.txt`.
- Next Session path:
  1. Nothing mandatory. The digest chain (CLI `chatlog report graph` → POST /api/v1/graph/digest → `reports/graph-digest-<start>_<end>.md`) is live-verified on 127.0.0.1:5030 (2026-06-12 16:44, failed count unchanged at 267).
  2. If user asks to commit: scope to digest files only — `internal/chatlog/temporalgraph/digest*.go`, `internal/chatlog/http/graph.go`, `internal/chatlog/http/route.go`, `cmd/chatlog/cmd_report.go`, `docs/graph-digest.md`, `scripts/ralph/prd.json`, `scripts/ralph/progress.txt`, root state files. Exclude `reports/`, `openai_prmpt.md`, `reports.backup-*`, and pre-existing dirty files not part of this PRD.
  3. Optional user-triggered follow-up: one real `chatlog report graph --days 7 --summary` run (quota-sensitive); verify only `summary_used` + file size, never print summary content.
  4. DONE (2026-06-12): completed PRD archived to `archive/2026-06-12-graph-knowledge-digest/` (prd.json + full progress.txt); `scripts/ralph/prd.json` reset to bootstrap placeholder; `scripts/ralph/progress.txt` reset keeping `## Codebase Patterns` + digest-run learnings. To start new work: write a new PRD via `tasks/` and regenerate `scripts/ralph/prd.json` from it.
- Service note (2026-06-12): the service binary was rebuilt (`make build`) and restarted to pick up the digest route (old pid 18883 SIGTERMed; user restarted externally with `serve --config-dir .cache/daily-report-config`). Health and graph status verified after restart.

## Previous Objective (2026-06-10, superseded)

- Goal: execute the NEW active PRD `scripts/ralph/prd.json` — branch `ralph/graph-knowledge-digest`, 6 stories, all `passes=false`. Next executable item is **US-001** (temporalgraph manager read-only `Digest` aggregation + tests).
- Feature: `graph-knowledge-digest-2026-06-10` in `feature_list.json` (active). Source PRD: `tasks/prd-graph-knowledge-digest.md`. Key constraints: default path zero model calls; `summary=true` ≤1 role-neutral Chat call with fallback (mock-tested only); digest must not touch the graph queue; verify outputs by path/size/mtime/section-count only.
- User identity note (2026-06-10): user is a traditional telecom ops engineer — do NOT embed the `openai_prmpt.md` "AI blogger" persona into any prompt or template.
- `reports` is a symlink to `/Volumes/WorkSSD/Dev/openclaw_mz/knowledge/raw/微信每日聊天记录`; `?? reports` in git status is pre-existing and must never be committed.
- Previous run `ralph/temporal-graph-failed-reprocess-parallelism-2026-06-06` finished 13/14 (`US-005` blocked, reason documented) and is archived under `archive/2026-06-10-temporal-graph-failed-reprocess-parallelism/`. Feature `temporal-graph-reprocess-failed-2026-06-04` is now `monitoring`: the background worker keeps draining the queue; check `GET /api/v1/graph/status` if throughput matters.

## Previous Objective (2026-06-07, superseded)

- Goal: drive the previous PRD (14 stories) forward. US-001..US-004 are `passes=true`; US-005 is `blocked=true` (10-min convergence unreachable); US-006/US-007 are marked superseded in `notes` (see HA-004 / HA-007); HA-001..HA-007 all reached `passes=true` by 2026-06-10.
- Subsystem: Temporal graph / semantic Chat provider / runtime graph worker / HA guard.
- Current status: 2026-06-07 HA recovery restored `serve --config-dir`, injected 5 MiniMax keys into tmux, requeued recoverable failures, and graph worker is running with extraction `workers=5`. Latest status: `running=true`, `source_count=21081`, `pending=11037`, `processing=8`, `processed=10019`, `failed=17`, `workers=5`, `enqueue_workers=1`, `last_error` empty, semantic `mmx`/`MiniMax-M2.7`/`4096`/`0.2`/`has_api_key=true`, live process key count 5.

## Files

- `feature_list.json`: active feature is `temporal-graph-reprocess-failed-2026-06-04` with runtime evidence; appended side task `middleware-incident-chain-report-2026-06-04`.
- `progress.md`: current status, verification evidence, risks, and next monitoring command.
- `session-handoff.md`: this restart note.
- `reports/middleware-incident-chain-2026-05-31_06-03.html`: side-task HTML retrospective, private (not committed).
- `internal/chatlog/temporalgraph/manager.go`: graph decode retry and per-source timeout hardening for 5-worker runtime.
- `internal/chatlog/temporalgraph/helpers.go`: graph prompt redaction/cropping before model calls.
- `internal/chatlog/semantic/client.go`: MiniMax 1026/1027 sensitive errors are non-retryable and do not rotate keys.
- `internal/chatlog/temporalgraph/manager.go`: `recoverableGraphTimeoutErrorTokens` now includes `before request` for resumable timeout errors.
- `cmd/chatlog/cmd_serve.go`: hidden HTTP service launcher for tmux/service config startup.
- `internal/chatlog/manager.go`: server startup config logging no longer prints raw config structs.

## Runtime State

- Service: `http://127.0.0.1:5030`.
- Store: `/Volumes/HDD/chatlog/wxid_qonry7vlh3vt22_d68e/.chatlog_graph/temporal_graph.db`.
- Before retry: `running=false`, `pending=0`, `processed=4849`, `failed=1814`.
- Failure bucket summary: `1772` old `chat model is not configured` failures, plus decode/422 style failures.
- Semantic Chat config was changed to `chat_provider=mmx`, `chat_model=MiniMax-M3`, `chat_max_tokens=8192`, `chat_temperature=0.2`.
- After `graph/resume`: `pending=1772`, `failed=42`.
- After `graph/rebuild reset=false`: follow-up status showed `enqueue_running=true`, `source_count=6964`, `pending≈2075`, `processing=8`, `failed=41`.
- Latest status after harness check: `running=true`, `enqueue_running=true`, `source_count=9891`, `pending=5001`, `processing=8`, `processed=4850`, `failed=35`.
- 2026-06-05 concurrency update: graph config set to `workers=5`, `enqueue_workers=4`; SQLite `graph_meta` persisted the same values.
- 2026-06-05 tmux environment update: `MINIMAX_API_KEYS` was explicitly injected into the `chatlog-alpha` tmux session before restart. Live process key-count check reported `pid_env_key_count=5`; secret values were not printed.
- 2026-06-05 stability update: malformed graph JSON responses now retry up to 3 decode attempts; each source has a 10 minute total processing timeout to avoid indefinite worker starvation.
- Latest status after rebuild/restart/resume: `running=true`, `source_count=17063`, `pending=12145`, `processing=9`, `processed=4892`, `failed=17`, `workers=5`, `enqueue_workers=4`, `last_error` empty.
- 2026-06-05 prompt privacy update: graph prompt payload removes raw `talker_id` / `sender_id`, sanitizes names, caps participants/entity hints, whitelists metadata, trims context to 2 before + target + 1 after, and truncates long message content before Chat calls.
- 2026-06-05 sensitive error update: `input new_sensitive (1026)` and `output new_sensitive (1027)` are non-retryable; current failed bucket check kept `sensitive_1026=1` and `sensitive_1027=4` failed while requeueing timeout/config errors.
- Latest status after prompt/sensitive hardening and final monitor: `running=true`, `source_count=17063`, `pending=12134`, `processing=1`, `processed=4911`, `failed=17`, `workers=5`, `enqueue_workers=4`, `last_error` empty.
- 2026-06-05 20:37 incident check: HTTP health was OK, but graph status returned `running=false`, queue counters `0`, and `last_error=sql: database is closed`. Readonly SQLite proved the queue was still present: `done=4951`, `failed=62`, `pending=12047`, `processing=3`; failed buckets were `context_deadline_exceeded=45`, `json_decode_or_format=12`, `sensitive_1027=4`, `sensitive_1026=1`.
- 2026-06-05 20:41 recovery: respawned `chatlog-alpha` tmux pane with `./bin/chatlog serve --config-dir .cache/daily-report-config`, verified service listening on `127.0.0.1:5030`, verified live process still had `pid_env_key_count=5`, and ran `POST /api/v1/graph/resume?format=json`.
- Latest status after 20:41 recovery and quick gate: `running=true`, `source_count=17355`, `pending=12357`, `processing=1`, `processed=4980`, `failed=17`, `workers=5`, `enqueue_workers=4`, `last_error` empty. Timeout failures were requeued; remaining failed bucket is `json_decode_or_format=12`, `sensitive_1027=4`, `sensitive_1026=1`.
- Current semantic runtime config after service-config restart: `chat_provider=mmx`, `chat_model=MiniMax-M2.7`, `chat_max_tokens=4096`, `chat_temperature=0.3`, `has_api_key=true`. This differs from earlier runtime evidence for `MiniMax-M3`/`8192`/`0.2`; treat the HTTP config as live truth.
- 2026-06-05 21:09 persistent M3 switch: graph was paused until `processing=0`, `.cache/daily-report-config/chatlog-server.json` was backed up to `.cache/daily-report-config/chatlog-server.json.bak-20260605_210932`, both service config and HTTP runtime were set to `chat_provider=mmx`, `chat_model=MiniMax-M3`, `chat_max_tokens=8192`, `chat_temperature=0.2`, and graph was resumed.
- Latest status after M3 switch: `running=true`, `source_count=17355`, `pending=12107`, `processing=19`, `processed=5210`, `failed=19`, `workers=5`, `enqueue_workers=4`, `last_error` empty. Runtime process still reports `pid_env_key_count=5`. Failed buckets after the switch: `json_decode_or_format=12`, `sensitive_1027=5`, `sensitive_1026=1`, and one MiniMax POST error bucket; failed count did not increase during the 60 second post-resume monitor.
- 2026-06-05 lease-context hardening follow-up: `acquireMiniMaxLeaseContext` and `before request` bucket were added, and `go test ./internal/chatlog/semantic ./internal/chatlog/temporalgraph` + `./init.sh` pass; latest status `running=true`, `source_count=17355`, `pending=12087`, `processing=17`, `processed=5225`, `failed=26`, `workers=5`, `enqueue_workers=4`, `last_error` empty.
- 2026-06-06 20:22 user-requested reprocess: pre-reprocess status was `running=false`, `source_count=21073`, `processed=7584`, `pending=0`, `processing=0`, `failed=13489`, `progress_pct=100`. Today's `+0800` failed bucket was dominated by `chat_model_not_configured=13473`; the visible file log `logs/chatlog-alpha-20260606_090057.log` was `0B`.
- Runtime action: ran `POST /api/v1/graph/resume?format=json` with current semantic config `mmx` / `MiniMax-M2.7` / `4096` / `0.2` and graph config `workers=1`, `enqueue_workers=1`.
- Latest status after short monitor, quick gate, and final poll: `running=true`, `source_count=21073`, `pending=13456`, `processing=9`, `processed=7595`, `failed=13`, `workers=1`, `enqueue_workers=1`, `last_error` empty. Remaining failed buckets: `json_decode_or_format=10`, `sensitive_1027=3`.
- 2026-06-07 HA incident: live runtime had drifted to `chat_provider=glm`, `chat_model=glm-5.1`, `has_api_key=false`, causing `last_error="chat model is not configured"`, while `.cache/daily-report-config/chatlog-server.json` still had `mmx` / `MiniMax-M2.7` with an API key.
- Runtime action: injected 5 `MINIMAX_API_KEYS` from zsh into the `chatlog-alpha` tmux session without printing secrets, respawned the pane as `./bin/chatlog serve --config-dir .cache/daily-report-config`, verified `pid_minimax_key_count=5`, then ran `POST /api/v1/graph/resume?format=json`.
- Latest HA status after quick gate: `running=true`, `source_count=21081`, `pending=11037`, `processing=8`, `processed=10019`, `failed=17`, `workers=5`, `enqueue_workers=1`, `last_error` empty. Remaining failed buckets: `json_decode_or_format=10`, `sensitive_1027=7`.
- New file: `scripts/chatlog-ha-guard.sh` provides one-shot or looped self-healing for health/config drift, tmux key injection, graph config alignment, and recoverable graph resume.

## Verification Evidence

- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/health'`
  - Result: PASS.
- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/graph/status?format=json'`
  - Result: PASS, status read successfully.
- Command: readonly SQLite failed bucket query
  - Result: PASS, error buckets counted without printing private source content.
- Command: `POST /api/v1/semantic/config?format=json`
  - Result: PASS, MiniMax-M3 graph Chat config saved without sending secret values.
- Command: `POST /api/v1/graph/resume?format=json`
  - Result: PASS, recoverable failures requeued.
- Command: `POST /api/v1/graph/rebuild?format=json` with `reset=false`
  - Result: PASS, enqueue accepted and began expanding the pending queue.
- Command: `POST /api/v1/graph/config?format=json` with `{"workers":5,"enqueue_workers":4}`
  - Result: PASS, config persisted and runtime status reported `workers=5`.
- Command: process environment key-count check
  - Result: PASS, live `bin/chatlog` process had 5 MiniMax keys visible.
- Command: `go test ./internal/chatlog/dailyreport ./internal/chatlog/semantic ./internal/chatlog/temporalgraph`
  - Result: PASS after graph runtime hardening.
- Command: runtime monitor after restart/resume
  - Result: PASS, processed advanced to `4892`, failed stayed at `17`, and `last_error` stayed empty during the final short monitor.
- Command: temporal graph prompt redaction/cropping test
  - Result: PASS, raw IDs are omitted from prompt payload and oversized context/participant/entity data is capped before model calls.
- Command: MiniMax sensitive retry test
  - Result: PASS, `output new_sensitive (1027)` made one request with two keys configured and did not switch keys; 1026/1027 share the same non-retryable matcher.
- Command: `POST /api/v1/graph/resume?format=json` after hardening plus readonly SQLite bucket query
  - Result: PASS, `failed` dropped from `40` to `17`; `timeout_or_config=0`, `sensitive_1026=1`, `sensitive_1027=4`.
- Command: runtime status after 20 second monitor
  - Result: PASS, `running=true`, `workers=5`, `enqueue_workers=4`, `processed=4911`, `processing=1`, `failed=17`, `pending=12134`, `last_error` empty.
- Command: `jq empty feature_list.json && ./init.sh`
  - Result: PASS, root harness check `77/77`, harness skill check `29/29`, daily report help, and HTTP endpoint list passed.
- Command: `go test ./internal/chatlog/semantic ./internal/chatlog/temporalgraph`
  - Result: PASS, acquire-context helper and `before request` token updates.
- Command: incident status check at 2026-06-05 20:37
  - Result: PASS but unhealthy graph manager state: `last_error=sql: database is closed`; direct SQLite queue counts remained nonzero.
- Command: tmux respawn, `graph/resume`, 30 second monitor, final status poll, and `./init.sh`
  - Result: PASS, recovered graph manager, requeued `45` timeout failures, kept sensitive failures unretried, quick gate passed, and latest status is `running=true`, `processed=4980`, `processing=1`, `pending=12357`, `failed=17`, `last_error` empty.
- Command: persistent MiniMax-M3 switch flow
  - Result: PASS, graph pause drained the active batch, service config backup was written, `.cache/daily-report-config/chatlog-server.json` and live HTTP config both read `MiniMax-M3`/`8192`/`0.2`, graph resumed with `workers=5`, live process kept `pid_env_key_count=5`, and `./init.sh` quick gate passed.
- Command: 2026-06-06 failed-source reprocess flow
  - Result: PASS, today's logs/status were checked, readonly SQLite buckets were counted without private content, `POST /api/v1/graph/resume?format=json` requeued recoverable failures, `failed` dropped from `13489` to `13`, and a short monitor showed `processed` advancing from `7584` to `7586`.
- Command: `jq empty feature_list.json && ./init.sh`
  - Result: PASS, `feature_list.json` parsed, root harness `80/80`, skill harness `29/29`, daily report help, and HTTP endpoint list passed.
- Command: 2026-06-07 HA recovery flow
  - Result: PASS, restored `serve --config-dir`, verified live semantic MMX config, verified tmux/process MiniMax key count `5`, resumed graph, set `workers=5`, and added `scripts/chatlog-ha-guard.sh` with `bash -n`, dry-run, and idempotent run checks.
- Command: `jq empty feature_list.json && ./init.sh`
  - Result: PASS, `feature_list.json` parsed, root harness `80/80`, skill harness `29/29`, daily report help, and HTTP endpoint list passed.

## Not Verified

- Full completion of the reprocessed queue: not waited.
- Private failed source contents: not inspected.
- Full completion under 5-worker graph concurrency: not waited; only short runtime stability was verified.
- Commit/push: not performed.

## Blockers

- None for requeueing.

## Risks

- MiniMax-M3 may be weak for strict JSON extraction in this prompt path. It already produced a truncated response once; `chat_max_tokens` was raised to `8192`.
- Running worker consumes MiniMax quota.
- Current graph extraction can run up to 5 concurrent sources because the tmux-launched process now sees 5 MiniMax keys.
- MiniMax upstream response latency is high. The 10 minute source timeout prevents indefinite worker starvation, but timeout failures may still accumulate if upstream remains slow.
- If errors accumulate, do not keep blindly retrying; bucket the new failures first.
- Remaining failed rows include non-retryable sensitive blocks and decode/raw-output failures. Do not requeue them blindly; bucket first and decide whether sanitized prompt regeneration or parser tightening is needed.
- Service config now loads `MiniMax-M3`/`8192`/`0.2`; backup for rollback is `.cache/daily-report-config/chatlog-server.json.bak-20260605_210932`.
- Side task: `reports/middleware-incident-chain-2026-05-31_06-03.html` is a private report. Do not commit the `reports/` directory; the report is a snapshot of the graph at the time of writing and will go stale as the worker drains `pending=12147`.

## Next Session

Recommended Next Step:
- 下一个可执行项是 `HA-001`(priority=8)。可走两条路:
  - **Ralph 自动**: `PYTHONDONTWRITEBYTECODE=1 python3 -B scripts/ralph/ralph.py codex` 跑一轮 developer/validator,Ralph 会自然取 HA-001(已是最小未结+最高 priority)。`passes=true` 后 ralph.py 会自动 commit。
  - **手动接管**: 按 `tasks/prd-temporal-graph-ha-keypool.md` 第 9 节 Verification Plan 顺序执行,验证后把 `passes=true` 写进 prd.json,然后跑 quick gate。
- 在做 HA-001 之前,先用以下命令看 live truth(2026-06-07 12:48 latest):
  `curl -sS 'http://127.0.0.1:5030/api/v1/graph/status?format=json' | jq '{running,workers,processed,processing,pending,failed,last_error}'`
  `curl -sS 'http://127.0.0.1:5030/api/v1/semantic/config?format=json' | jq '{chat_provider,chat_model,has_api_key}'`
- 不要手动 requeue `json_decode_or_format=10` / `sensitive_1027=7` 这 17 条 — HA-004 / HA-007 会在 evidence 上把"为什么不 requeue"用 AC 闭环。
- 旧 US-006 / US-007 状态处理:
  - 当前是 `passes=false, blocked=false, notes=Superseded by HA-xxx`(留审计链)。
  - HA-004 / HA-007 跑完后,会话来手动写 `passes=true` + notes "completed as part of HA-xxx";`ralph.py` 当前不自动读 `notes` 字段决定 PASS,所以需要这一步手收。
  - 如果 `ralph.py` 升级了 `notes starts with "Superseded by" → skip` 逻辑,则无需手动处理。

Use `scripts/chatlog-ha-guard.sh` for one-shot recovery, or run it with `--loop 60` in tmux for continuous guarding. Do not manually requeue `json_decode_or_format` or `sensitive_1027` rows without first deciding parser/safety handling.

## Ralph Automation Handoff

- Added `.agents/`, `.claude/`, and `scripts/ralph/` for PRD-driven AI coding automation.
- Current `scripts/ralph/prd.json` is a completed bootstrap placeholder. Replace it with a real PRD-derived story list before running Ralph for product work.
- Current `scripts/ralph/progress.txt` contains chatlog-specific Codebase Patterns and the bootstrap record.
- Developer agent prompt: `scripts/ralph/CLAUDE.md`.
- Validator prompt: `scripts/ralph/VALIDATOR.md`.
- Runner entrypoints:
  - `python3 scripts/ralph/ralph.py`
  - `python3 scripts/ralph/ralph.py codex`
  - `PYTHONDONTWRITEBYTECODE=1 python3 -B scripts/ralph/ralph.py --check`
- Commit behavior: developer and validator agents must not commit directly; `scripts/ralph/ralph.py` auto-commits only after Validator success.
- Commit safety: startup-dirty paths and private/cache outputs are excluded, including `reports/`, `.cache/`, `logs/`, `outputs/`, `.env*`, `*.log`, `*.tmp`, `*.pyc`, and `__pycache__`.
- Verification passed: no copied cache/system files, Ralph `--check`, root harness `77/77`, repo-local skill harness `29/29`, and `./init.sh`.
- Not run: full autonomous Ralph loop, because no real product PRD was provided in this turn.

## AGENTS Rules Refresh Handoff

- Refreshed root `AGENTS.md` using `.agents/skills/source-command-create-rules/SKILL.md`.
- `AGENTS.md` now documents the repo as a Go local-tool product with CLI, TUI, HTTP API, daily report, semantic/LLM provider, temporal graph, Hermes push, WeChat DB/key access, and Ralph automation subsystems.
- Recorded this side task in `feature_list.json` as `global-agents-rules-refresh-2026-06-05`.
- Latest create-rules refresh also documents project-local `.agents/skills/` entries: `prime`, `plan-feature`, `create-rules`, and `source-command-create-rules`; no global skill directory was modified.
- 2026-06-06 create-rules refresh clarified that Ralph automation must inspect `scripts/ralph/prd.json` `userStories` before running, `reports.backup-*` is private generated output like `reports/`, and `archive/` / `tasks/` are scoped work artifacts.
- Verification passed:
  - `node scripts/check-root-harness.mjs`: root harness `77/77`.
  - `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs`: skill harness `29/29`.
  - `./init.sh`: quick gate passed with daily report help and HTTP endpoint list.
  - Latest create-rules refresh: `jq empty feature_list.json`, root harness `80/80`, skill harness `29/29`, and `./init.sh` quick gate all passed.
  - 2026-06-06 create-rules refresh: `jq empty feature_list.json`, `node scripts/check-root-harness.mjs` (`80/80`), `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs` (`29/29`), and `./init.sh` quick gate passed.
- Not run:
  - `./init.sh --full`, because this was documentation/rules-only work.
  - `./init.sh --runtime`, because no live runtime behavior was changed or claimed.
- 2026-06-06 refresh did not run `./init.sh --full` or `./init.sh --runtime`; it was rules/state documentation only.
- Next Session: if continuing runtime graph work, resume from the temporal graph monitoring command above; if continuing harness work, inspect `AGENTS.md`, `feature_list.json`, `progress.md`, and this handoff first.

## Project Command Skills Handoff

- Converted `.agents/commands` shortcuts into project-local skills under `.agents/skills/`.
- New local skills:
  - `.agents/skills/prime/SKILL.md`
  - `.agents/skills/plan-feature/SKILL.md`
  - `.agents/skills/create-rules/SKILL.md`
- Existing `.agents/skills/source-command-create-rules/SKILL.md` was preserved, so both `create-rules` and `source-command-create-rules` entry names exist locally.
- Original `.agents/commands/*.md` files were not deleted or moved.
- `scripts/check-root-harness.mjs` now verifies these three local skill files.
- Verification passed:
  - `find .agents/skills -maxdepth 2 -name 'SKILL.md' -print | sort`
  - `node scripts/check-root-harness.mjs`: root harness `80/80`
  - `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs`: skill harness `29/29`
  - `jq empty feature_list.json`
- Not verified:
  - Current UI skill picker refresh. If these skills do not appear immediately, start a new Codex turn/session or reload the project so local `.agents/skills` is re-indexed.
- Scope boundary: no global `~/.agents` or `~/.codex/skills` files were written.

## Runtime Recovery Handoff: Temporal Graph Tail Completion 2026-06-06

- Goal: stabilize and finish the active temporal graph tail.
- Final status: completed active queue.
  - API status: `running=false`, `paused=false`, `workers=1`, `enqueue_workers=1`, `source_count=21036`, `processed=7547`, `processing=0`, `pending=0`, `failed=13489`, `progress_pct=100`, `last_error=null`, `last_updated_at=2026-06-06T16:15:03+08:00`.
  - SQLite status: `done=7547`, `failed=13489`.
  - Failed bucket summary: `chat_model_not_configured=13473`, `decode_graph_extraction_failed=9`, `minimax_sensitive_1027=3`, `context_deadline_exceeded=2`, `before_request_timeout=1`, `other=1`.
- Runtime actions taken:
  - Paused/controlled the running graph worker and avoided `graph/resume` because it would requeue all `chat model is not configured` failures.
  - Copied live MiniMax keys into the `chatlog-alpha` tmux session without printing secret values; verified `MINIMAX_API_KEYS_count=5`.
  - Respawned `chatlog-alpha` as `./bin/chatlog serve --config-dir .cache/daily-report-config`.
  - First tried persisted `MiniMax-M3`/`8192`/`0.2` with `workers=2`; it stayed stable but did not progress fast enough.
  - Switched tail mode to `MiniMax-M2.7`/`4096`/`0.2` with `workers=1`, then the tail drained to zero pending/processing.
- Config backups:
  - `.cache/daily-report-config/chatlog-server.json.bak-20260606_155428`: backup before aligning service config to M3.
  - `.cache/daily-report-config/chatlog-server.json.bak-20260606_160521-m27-tail`: backup before switching to M2.7 tail mode.
- Current persisted service config is `MiniMax-M2.7`/`4096`/`0.2` intentionally for stable tail completion.
- Next recommended step:
  - Do not blindly requeue `13473` config failures.
  - If reprocessing those failures is desired, first decide staged batches, target model, concurrency, quota budget, and whether to keep M2.7 fast mode or restore M3 quality mode.
  - Minimal verification command:
    `curl -sS 'http://127.0.0.1:5030/api/v1/graph/status?format=json' | jq '{running,processed,processing,pending,failed,last_error}'`

## PRD Merge Handoff: HA Keypool & Adaptive Routing 2026-06-07

- Source PRD: `tasks/prd-temporal-graph-ha-keypool.md` (174 行, 7 个 User Story, 10 个 FR, 5 个 Non-Goal, Verification Plan, 4 个 Open Questions).
- Action taken: 7 个 Story 拆 JSON append 到 `scripts/ralph/prd.json`,priority 8..14,每个含 5 条 AC + `sourcePrd` 引用 + (HA-004 / HA-007) `supersedes` 旧 story id。旧 US-006 / US-007 在 `notes` 写 superseded 原因,`passes` / `blocked` 保持。
- Privacy: PRD 拆 JSON 过程未把任何 `sk-...` 真实 API key 写入 prd.json;HA Story 的脱敏要求由 AC 条款约束(状态接口只返 label,benchmark fixture 禁止真实聊天正文)。
- 备份: `/tmp/prd.backup.json` 是入仓前 7-story 版本(8446 字节)。
- 验证命令与结果:
  - `jq '.userStories | length'` → `14` ✅
  - `jq -r '.userStories[] | .id'` → US-001..US-007, HA-001..HA-007 ✅
  - `jq '.userStories[7:14] | map({id, ac, src, sup})'` → 7 个 HA 全部 5 条 AC, `sourcePrd` 就位, HA-004 / HA-007 含 `supersedes` ✅
  - `./init.sh` quick gate → "Verification Complete" ✅
- 未验证:
  - `./init.sh --full`(本轮仅文档级改动,quick gate 足够)
  - 旧 US-006 / US-007 的 `passes` 字段未翻 true(留给 HA-004 / HA-007 通过后人工/ralph.py 处理)
- 下一个 Ralph 可执行项: **HA-001**(priority=8)。其余 HA-002..HA-007 按 priority 顺序进入队列。旧 US-001..US-004 已 passes=true, US-005 已 blocked=true, US-006/US-007 因 superseded notes + 后置 priority 处于"会先于 HA-001 被命中"风险;若 ralph.py 读 `notes` 跳过 superseded 则无问题,否则需要后续手动调整 priority 或 `passes=true`。
- 关联文档: `progress.md` 顶部 Current State 已加 PRD 合并摘要 + Next Session 段,底部 Side Task 段记录本次合并 evidence;`scripts/ralph/progress.txt` 顶部 Codebase Patterns 新增 1 条可复用 pattern,底部 12:48 段记录留证。

## PRD Story HA-001 Handoff: Runtime Chat readiness observable + self-healable 2026-06-07

- 状态: `HA-001.passes = true`(在 `scripts/ralph/prd.json` 中)。其余 HA-002..HA-007 仍 `passes=false / blocked=false`,下一个 ralph 命中 HA-002。
- 关键文件:
  - `internal/chatlog/semantic/client.go` — `MiniMaxKeyPoolStatus()` / `Snapshot()` / `classifyMiniMaxErrorBucket` / pool 计数(recordLease/recordError/redactedSignature)。
  - `internal/chatlog/http/route.go` — `GET /api/v1/semantic/mmx/status?format=json`(`handleSemanticMMXStatus`);`/api/v1/semantic/config?format=json` 增加 `configured_key_count` 与 provider-aware `has_api_key`。
  - `internal/chatlog/semantic/client_test.go` — 新增 4 个测试(Snapshot 不泄漏 / busy-idle / 错误桶分类 / 空 err)。
  - `scripts/chatlog-ha-guard.sh` — 新 `runtime_minimax_status`、`restore_semantic_config`、`load_minimax_env` zshenv/zshrc 兜底、`mmx_key_underprovisioned` 自愈分支。
- 端到端验证(alt port 5099/5098 临时实例,验证后已清理):
  - 好配置: `/api/v1/semantic/config?format=json` → `chat_provider=mmx, has_api_key=true, configured_key_count=5`。
  - 隐私: response grep `sk-` 0 命中;mmx status `key_labels=***1111..***5555, signature=sha256 短哈希`。
  - 自愈: 故意 `chat_provider=ollama, api_key=""` 起实例,跑 guard → 3 秒内 `WARN semantic_config_drift → ACTION restore_semantic_config → semantic provider=mmx has_api_key=true → mmx_status configured=5`。
- 未验证: 生产 chatlog-alpha (pid 45957) 仍是旧 binary,未重启;HA-002..HA-007 推进时按需要重启。
- 后续:
  - HA-002 复用 mmx status 端点做 key 隔离/切换。
  - HA-003 复用 `mmx_status.retry_count / last_error_bucket` 派生自适应 worker 阈值。
  - HA-006 / HA-007 切生产 binary 到本次构建,验证端到端。

## PRD Story HA-002 Handoff: MiniMax 5-key pool status can be inspected safely 2026-06-07

- 状态: `HA-002.passes = true`, `notes = ""`, `retryCount = 0`(在 `scripts/ralph/prd.json` 中)。下一个 ralph 命中 = **HA-003** (priority=10, Graph workers adapt to key-pool health)。
- 第 1 次验证失败原因: snapshot 端已暴露 6 个 quarantine 字段,但 `handleSemanticMMXStatus` 的 `gin.H` literal 没把它们 spread 进去,HTTP `/api/v1/semantic/mmx/status?format=json` 响应中没有 `quarantined_*` 任何字段,AC#4 "状态接口显示该 key label 进入隔离" 字面要求未满足。
- 关键文件:
  - `internal/chatlog/http/route.go` `handleSemanticMMXStatus` — `gin.H` 响应增加 6 字段:`quarantined_key_count` / `healthy_key_count` / `quarantined_labels` / `last_quarantined_label` / `last_quarantined_for` / `last_quarantined_at`(snapshot 端不需要改)。
  - `internal/chatlog/semantic/client.go` — HA-002 第 1 次提交时已实现 `Snapshot()` / `recordError` / pool-level quarantine,本轮未改。
  - `internal/chatlog/semantic/client_test.go` — HA-002 第 1 次提交时新增 4 个测试(quarantine 行为 + 脱敏),本轮未改。
- 端到端验证(alt port 5097,5 个 dummy `MINIMAX_API_KEYS`,验证后清理):
  - fresh 状态: `quarantined_key_count=0, healthy_key_count=0, quarantined_labels=[]`,`configured_key_count=5`。
  - 触发 `POST /api/v1/semantic/test` 后: `quarantined_key_count=5, healthy_key_count=0, quarantined_labels=[key_1..key_5], last_quarantined_label=key_2, last_quarantined_for=minimax_auth_error`,字面满足 AC#4。
  - response grep `sk-cp-` 0 命中,label 形态 `key_1..key_5`,满足 AC#2 脱敏要求。
  - `go test ./internal/chatlog/semantic` PASS,`./init.sh` quick gate PASS。
- 未验证: 生产 chatlog-alpha (pid 45957) 仍是旧 binary,未重启;HA-002 的端到端 quarantine 在 alt port 临时实例上验证。生产 binary 升级留给 HA-006 / HA-007。
- 后续:
  - HA-006 guard 可读 `quarantined_key_count` / `healthy_key_count` 做 "key count=5 但 healthy < 5" 告警。
  - HA-003 复用 `quarantined_key_count` / `retry_count` 派生自适应 worker 阈值。
  - HA-006 / HA-007 切生产 binary 后做完整端到端验证。

## Runtime Reprocess Handoff: Temporal Graph ollama_refused Recovery 2026-06-08

- User request: reprocess failed graph rows from status `source_count=21144`, `processed=17018`, `failed=4126`, `pending=0`.
- Runtime truth at start:
  - Service is `./bin/chatlog serve --config-dir .cache/daily-report-config`.
  - Semantic config is `mmx` / `MiniMax-M2.7` / `4096` / `0.2` with `has_api_key=true`.
  - HA guard dry-run reports MiniMax key count `5` and mmx status `configured=5`.
- Failed bucket truth before requeue:
  - `ollama_refused=3951` (`Post http://127.0.0.1:11434/api/chat: connect refused`) from the 2026-06-07 provider-drift window.
  - `minimax_401=141`.
  - `sensitive_1027=18`.
  - `json_decode_or_format=10`.
  - `zero_key_attempts=5`.
  - `other=1`.
- Action taken:
  - Set graph workers to `3` and enqueue workers to `1`.
  - Existing `/api/v1/graph/resume` did not move these rows because `ollama_refused` is not in the built-in recoverable buckets.
  - Directly requeued only `ollama_refused` rows:
    - Canary: 10 rows, 9 processed during the first observation window, `failed` did not rise.
    - Bulk: remaining 3941 rows.
  - Did not requeue `minimax_401`, `sensitive_1027`, `json_decode_or_format`, `zero_key_attempts`, or `other`.
- Latest status after requeue:
  - API: `running=true`, `pending=3911`, `processing=8`, `processed=17050`, `failed=175`, `workers=3`, `enqueue_workers=1`, `last_error=null`, `last_updated_at=2026-06-08T07:20:24+08:00`.
  - SQLite: `done=17050`, `failed=175`, `pending=3911`, `processing=8`.
- Next session:
  - Monitor:
    `curl -sS 'http://127.0.0.1:5030/api/v1/graph/status?format=json' | jq '{running,processed,processing,pending,failed,workers,last_error}'`
  - After pending drains, run a readonly failed bucket query. Do not blindly retry the remaining `175`; `minimax_401` needs key/account authorization triage, `sensitive_1027` should remain blocked unless policy changes, and `json_decode_or_format` needs parser/model-quality handling.
