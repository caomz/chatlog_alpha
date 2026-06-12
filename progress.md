# Progress

Last Updated: 2026-06-12

## Current State

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
