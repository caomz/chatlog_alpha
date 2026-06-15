# Codebase Concerns

**Analysis Date:** 2026-06-15

This document captures the active technical debt, known issues, and areas of concern for `chatlog_alpha` as observed in the current working tree, state files, and live runtime evidence. It is a real-tool audit, not a hypothetical review: each item maps to actual files, observed failures, and forward-looking impact.

---

## 1. Persistent Privacy / Quota / Secrets Risks

These areas touch private chat data, model provider calls, or API keys. The current safeguards are documented in `progress.md`; the gaps and failure modes are listed here so future work can prioritize the next round of hardening.

### 1.1 API key handling — well-controlled but single point of failure

- **Status:** Mitigated, but the mitigation is fragile and reverts easily.
- **Files / endpoints:**
  - `internal/chatlog/semantic/client.go` — `miniMaxAPIKeyPool`, `acquireMiniMaxLeaseContext`, `Snapshot()`, `MiniMaxKeyPoolStatus()`, error bucket classifier.
  - `internal/chatlog/http/route.go` — `handleSemanticConfigGet` / `handleSemanticConfigPost`, `handleSemanticMMXStatus`.
  - `scripts/chatlog-ha-guard.sh` — `load_minimax_env`, `runtime_minimax_status`, `restore_semantic_config`, key count redaction.
  - `.cache/daily-report-config/chatlog-server.json` — persisted provider config (contains `api_key` indirectly via env at runtime).
- **What's good:**
  - `/api/v1/semantic/mmx/status?format=json` returns `key_labels=key_1..key_5` and a `signature` short hash, never `sk-` prefixes (verified by `progress.md` 2026-06-07 evidence).
  - 1026/1027 sensitive errors are non-retryable, so content-safety blocks do not rotate keys.
  - `scripts/chatlog-ha-guard.sh --dry-run` redacts all `MINIMAX_API_KEYS` values.
- **Remaining concerns:**
  - **Drift between persisted service config and live process env** caused a 2026-06-07 outage: `chatlog-server.json` had `mmx`/`M2.7`/has_key, but `./bin/chatlog` was launched without `serve --config-dir`, so HTTP runtime drifted to `glm`/`glm-5.1`/`has_api_key=false`. The `chatlog-ha-guard.sh` script now self-heals, but the default `bin/chatlog` entry point (TUI default in `cmd/chatlog`) silently loses the env-injected key path.
  - **JSON serializer risk:** the `SemanticConfig` struct (`internal/chatlog/conf/semantic.go:34`) has a top-level `APIKey` field. Any future endpoint that returns the raw config struct (rather than the existing `gin.H` projection in `handleSemanticConfigGet`) would leak the key. The current `gin.H` literal only emits `chat_provider`, `has_api_key`, `configured_key_count`, etc. — but a future refactor that swaps to `c.JSON(200, config)` would re-introduce the leak.
  - **Backup files in `.cache/daily-report-config/`:** the 4 `chatlog-server.json.bak-*` files all contain provider/model settings. If a future change writes the live `api_key` into the persisted config (rather than reading from `MINIMAX_API_KEYS` env), those backups will retain key values indefinitely. Recommended: verify backups contain no `api_key` field, and add `.gitignore` exclusion for `*.bak-*` inside `.cache/` even though `.cache/` is already ignored.

### 1.2 MiniMax key pool exhaustion / 5-key under-provisioning

- **Issue:** When upstream begins returning 401 / auth errors for a key, `recordError` moves the key into pool-level quarantine (HA-002). With 5 keys quarantined, `configured_key_count=5` looks healthy but `healthy_key_count=0` and all graph extraction stalls.
- **Files:**
  - `internal/chatlog/semantic/client.go` — `miniMaxAPIKeyPool.tryAcquire`, `recordError`, `classifyMiniMaxErrorBucket`.
  - `internal/chatlog/temporalgraph/manager.go` — `bucketFromError` (substring match against upstream error strings).
  - `scripts/chatlog-ha-guard.sh` — `mmx_key_underprovisioned` self-heal.
- **Concerns:**
  - `bucketFromError` does substring match on English error text. If MiniMax changes wording (e.g. switches to "rate_limited" or "too many requests"), the adaptive worker tier (`adaptiveKeyErrorBuckets`) will silently fail to downgrade concurrency. Mitigated by: 3-bucket allow-list with `classifyMiniMaxErrorBucket` as the source of truth on the semantic side.
  - There is no proactive key health check — keys only move into quarantine on the next failing request. If all 5 keys are silently expired by the upstream account, the first 5 graph extractions will all 401-quarantine in sequence, and only then will the pool be fully empty.
  - The 5 keys live in `~/.zshrc` (`MINIMAX_API_KEYS`) — the runtime depends on the user re-injecting them into tmux after a respawn. The HA guard does this automatically, but a manually launched `./bin/chatlog` in a fresh terminal will not pick them up.

### 1.3 Graph store WAL mode not enabled — `SQLITE_BUSY` risk

- **Issue:** `internal/chatlog/temporalgraph/store.go:31` opens SQLite without `?_journal_mode=WAL&_busy_timeout=5000`. High-concurrency graph workers (5 concurrent) will block each other on writes, and the `/api/v1/graph/status` / digest / query endpoints may hit `SQLITE_BUSY` under load.
- **Status:** Open — recorded in `TODOS.md` as `TODO-2026-06-10-graph-store-wal`. Not yet executed because it requires a maintenance window with the queue drained and a `temporal_graph.db` backup.
- **Risk if not addressed:** status polling during graph extraction becomes a contention point; the 2026-06-05 20:37 incident (`last_error=sql: database is closed`) suggests WAL wouldn't have prevented that specific failure (it was a `Close()` race), but WAL would reduce contention-related failed commits in steady state.
- **Fix approach:** Add `?_journal_mode=WAL&_busy_timeout=5000` to the DSN at `internal/chatlog/temporalgraph/store.go:31`, with a `_busy_timeout=5000` floor to bound `SQLITE_BUSY` waits. Backup `temporal_graph.db` to a `.bak-YYYYMMDD_HHMMSS` file before the change (analogous to `.cache/daily-report-config/chatlog-server.json.bak-*`).

### 1.4 Daily report model calls — `chatlog report daily --vision` / `--summary` quota exposure

- **Status:** Documented as "do not run by default" in `AGENTS.md` and `scripts/ralph/CLAUDE.md`. `feature_list.json` `daily-report-2026-05-28` shows the 2026-05-28 vision/summary run consumed model quota and must not be repeated without explicit user authorization.
- **Files:**
  - `cmd/chatlog/cmd_report.go` — daily report CLI entry; --vision / --summary flags call into model providers.
  - `internal/chatlog/dailyreport/ai_analysis.go` — provider-agnostic analysis.
  - `internal/chatlog/dailyreport/vision.go` — image-vision path.
  - `internal/chatlog/dailyreport/dialogue_renderer.go` — privacy-bounded summarization.
- **Concerns:**
  - `dailyreport` package lacks a per-run cost guard. A user (or agent) running `chatlog report daily --vision --date today` with a wide date range will silently consume significant MiniMax vision quota and could exceed daily limits without warning.
  - The current "verify by path, size, timestamp, count" pattern in `progress.md` and the Ralph progress notes works, but only if the agent checks the inputs first. There's no runtime gate that rejects model calls outside the configured allow-list.
  - `reports/` is a **symlink** to `/Volumes/WorkSSD/Dev/openclaw_mz/knowledge/raw/微信每日聊天记录` (per `progress.md` 2026-06-10 entry). All daily HTML/Markdown report artifacts land in another repo's working tree. This is intentional but easy to miss — any future `git status` from `chatlog_alpha` will show `?? reports` as a pre-existing untracked line, masking any new `reports/` artifacts that should have been ignored.

---

## 2. Known Issues / Documented Bugs

These are open or recurring issues that have been observed in the live runtime or in the source code.

### 2.1 `IsSelf` determination in v4 message parsing is unreliable

- **Files:** `internal/model/message_v4.go:45`, `:63`
- **Issue:** Two `FIXME` comments mark the `IsSelf` determination (`Status == 2 || (!IsChatRoom && talker != UserName)`) as inaccurate. Status 2 means "sent", but for group chat, the relationship between `Status` and "is self" is not deterministic. The fix requires a `UserName` lookup against a `Name2Id` mapping that may not be available at the time of message parsing.
- **Impact:** Group chat message attribution can be wrong; downstream features (daily report "me" highlights, graph entity role) can mis-tag speakers.
- **Test coverage:** None — the message wrapping is exercised by integration tests via daily report and graph extraction, not via a focused unit test.
- **Fix approach:** Resolve via a pre-pass that builds the `Name2Id` map (similar to `internal/wechatdb/repository/message.go:112` "大量群聊用户名称重复" FIXME), then compare `m.UserName` against the local account's wxid.

### 2.2 v4 XML hardlink matching for media is broken

- **Files:** `internal/model/message_v4.go:93`
- **Issue:** `// FIXME 尝试解决 v4 版本 xml 数据无法匹配到 hardlink 记录的问题` — v4 messages carry `packedInfo` that should resolve media file paths on disk via hardlink tables, but the current implementation constructs a path from md5 alone (`msg/attach/<talkerMd5>/<YYYY-MM>/Img/<imageMd5>`) without verifying the hardlink exists.
- **Impact:** Daily report HTML renders for v4 messages may have broken `<img src>` URLs; media preview fails silently.
- **Fix approach:** Use the same hardlink resolution path that the v3 reader uses, or fetch the file list from `internal/wechatdb/wcdbapi/client.go` and verify the constructed path matches a real hardlink.

### 2.3 Group chat `GetContact` returns duplicate user names

- **Files:** `internal/wechatdb/repository/message.go:112`
- **Issue:** `// FIXME 大量群聊用户名称重复，无法直接通过 GetContact 获取 ID，后续再优化` — group chat members share display names, and the contact lookup returns the first match rather than disambiguating by group membership.
- **Impact:** Group chat speaker attribution is ambiguous when two members share a display name; graph extraction can conflate two speakers into one entity.
- **Fix approach:** Pass `group_id` to `GetContact` and use a (group_id, display_name) → wxid mapping. This needs a data source that maintains the per-group roster.

### 2.4 Media message HTM prefix (notes) unhandled

- **Files:** `internal/model/mediamessage.go:281`
- **Issue:** `// FIXME 笔记的第一条是 htm 数据，暂时跳过处理` — the first line of "note" media messages is HTML, which is currently dropped during parsing.
- **Impact:** Note-style messages have truncated content in daily reports and graph extraction.
- **Fix approach:** Parse the HTM prefix with `golang.org/x/net/html` or a simpler stripper that removes tags but preserves text.

### 2.5 Half-closed SQLite state — `last_error=sql: database is closed`

- **Files:** `internal/chatlog/temporalgraph/manager.go:182` (Close), `store.go:45` (Store.Close)
- **Issue:** The 2026-06-05 20:37 incident showed the graph manager could be left with `running=false, source_count=0, last_error=sql: database is closed` while the underlying SQLite file still contained thousands of `done` / `failed` / `pending` rows. Recovery required a tmux respawn.
- **Root cause hypothesis:** A `db.Close()` call somewhere in the manager path (potentially during a panic or an early return) leaves the `*sql.DB` in a closed state but doesn't mark the manager unhealthy. `Status()` reads from the closed handle and gets a zero-value error.
- **Status:** Mitigated by `chatlog-ha-guard.sh` self-heal, but the underlying race is not closed — a fresh incident could re-occur.
- **Fix approach:** Wrap `Status()` in a recover-to-healthy-state path that detects `sql.ErrConnDone` / `database is closed` and forces `m.store = OpenStore(m.conf.GetWorkDir())` re-open. Add a `--db-health` HTTP endpoint that returns `db_alive: true/false` and triggers re-open on false.

### 2.6 Graph worker starves on slow MiniMax upstream — recoverable timeout bucket

- **Files:** `internal/chatlog/temporalgraph/manager.go:76-81` (`recoverableGraphTimeoutErrorTokens`)
- **Issue:** MiniMax upstream latency is high. A 10-minute per-source timeout (`graphSourceTimeout`) prevents indefinite worker starvation, but slow requests can still tie up workers for the full window. The 2026-06-08 reprocess showed `chat_model_not_configured=13473` recovered to `pending=13466` and required multiple recovery cycles to drain.
- **Status:** Mitigated by token allow-list + `acquireMiniMaxLeaseContext` bounded acquisition (45s) + `before request` bucket. Still a soft cap.
- **Fix approach:** Make the timeout adaptive: if `tracker.Observe("minimax_timeout")` rate is high, the adaptive worker tier downgrades to 1, freeing capacity for fewer-but-faster requests. HA-003 implements this and is complete per the progress notes, but the live config drift from `2026-06-07` shows that the binary running in production may not be the latest one with HA-003 enabled.

### 2.7 Daily report `chatlog report daily --vision` regression risk

- **Files:** `internal/chatlog/dailyreport/vision.go` (large, 431 lines per commit `8ba9e182`)
- **Issue:** The vision module fetches image data from WeChat hardlinks and sends it to the chat provider. If the hardlink path is wrong (per the v4 hardlink FIXME above), the vision call sends a "broken" image and the upstream may either:
  - Return a model error (consumes quota, surfaces in `analysis_errors`).
  - Return a synthetic description of "broken image" (pollutes the report).
- **Status:** No regression test covers the vision path with a real `internal/wechatdb` fixture.

---

## 3. Test Coverage Gaps

Areas where the test surface is thin, risky changes could land unnoticed, and no runbook is documented.

### 3.1 No `internal/chatlog/hermespush/` test files

- **Path:** `internal/chatlog/hermespush/`
- **Status:** 6 Go files (`hermes_qq.go`, `hermes_qq_bridge.go`, `hermes_qq_bridge.py`, `hermes_weixin.go`, `hermes_weixin_bridge.go`, `hermes_weixin_bridge.py`) with no `_test.go` companion.
- **Risk:** Hermes push wires into an external `ilinkai.weixin.qq.com` (Weixin) / QQ bridge, and changes to the wire protocol (protobuf in `hermes_qq.go`, JSON in `hermes_weixin.go`) could silently break end-to-end push without a test catching it.
- **Fix approach:** Add fixture-based tests that exercise the bridge functions against captured wire data; do not make real HTTP calls in unit tests.

### 3.2 No `internal/wechat/key/darwin/` test files

- **Path:** `internal/wechat/key/darwin/`
- **Status:** Key scanning logic is platform-specific (`scanner_cgo_darwin.go`, `scanner_nocgo_darwin.go`, `scanner_others.go`, `v4.go`, `init_scan.go`) and untested. The `chatlog-key-tools` directory exists in `.cache/` (per `ls -la .cache/`) which suggests manual testing was used.
- **Risk:** Changes to the macOS key scanning path could break key acquisition silently, leading to the same 2026-06-07 "drift to glm/glm-5.1" outage recurring in a different form.

### 3.3 No test for the half-closed SQLite recovery path

- **Path:** `internal/chatlog/temporalgraph/manager_test.go` (referenced in commits, exists, but no test for the 2026-06-05 20:37 incident).
- **Risk:** A regression that re-introduces the half-closed state would not be caught by existing tests.

### 3.4 No e2e test for the digest endpoint

- **Path:** `internal/chatlog/temporalgraph/digest_test.go` exists and has 351 lines, but the test surface is in-memory; the HTTP path is only verified manually on the live `127.0.0.1:5030` service (per `progress.md` 2026-06-12).
- **Risk:** Future refactors of `internal/chatlog/http/graph.go` (`handleGraphDigest`) could break the metadata-only response or the idempotent overwrite semantics.

### 3.5 `internal/chatlog/http/` has 11 Go files; only `daily_report_test.go` (referenced) and `graph_test.go` (partial) have tests

- **Path:** `internal/chatlog/http/{daily_report.go,graph.go,hermes_qq.go,hermes_weixin.go,mcp.go,middleware.go,route.go,semantic_qa.go,service.go,sns_media.go,sns_wasm_assets.go}`.
- **Status:** `mcp.go`, `hermes_qq.go`, `hermes_weixin.go`, `middleware.go`, `sns_media.go` have no companion test files.
- **Risk:** Untested HTTP handlers, especially the MCP-style and middleware paths.

---

## 4. Fragile Areas — high blast-radius, low observability

These are subsystems where a small change can cause a large, hard-to-diagnose failure, and where the existing observability is limited.

### 4.1 Temporal graph worker / queue draining

- **Files:**
  - `internal/chatlog/temporalgraph/manager.go` — `Manager`, `ProcessPending`, `loadWorkerConfig`.
  - `internal/chatlog/temporalgraph/store.go` — `Store`, `init`, schema migrations.
  - `internal/chatlog/temporalgraph/helpers.go` — prompt redaction / cropping.
  - `internal/chatlog/temporalgraph/adaptive.go` — `keyHealthTracker`, `adaptiveMaxWorkers`.
  - `internal/chatlog/temporalgraph/buckets.go` (via `buckets_test.go`) — error bucket classifier.
- **Why fragile:**
  - Single SQLite file at `~/.chatlog_graph/temporal_graph.db` (or `<workDir>/.chatlog_graph/temporal_graph.db`). Concurrent writers + readers without WAL means `SQLITE_BUSY` is a steady-state possibility.
  - `ProcessPending` runs in a single goroutine, with a `wake chan struct{}` for early-wake. Closing the manager (`Close()` at `manager.go:182`) is straightforward, but the `db.Close()` call before the manager returns to the pool could leave in-flight workers holding a closed handle (the 2026-06-05 20:37 incident).
  - `loadWorkerConfig` reads `graph_meta` and clamps to `[1, maxGraphWorkers]`. If `maxGraphWorkers=12` is exceeded by a future config change, the clamp silently caps it; if the DB is corrupted, `fmt.Sscanf` returns 0 errors and the value stays at `defaultGraphWorkers=1`.
  - The `ResetProcessingSources()` call at `manager.go:131` is a recovery primitive (resets `processing=1` rows back to `pending`). It runs on every `NewManager`, but if a manager is started while another instance is already running, this could clobber the other instance's processing state.
- **Safe modification:** Change schema via additive migrations; never change `graph_source_records` column types. Test on a copy of `temporal_graph.db`. Coordinate binary restarts with the HA guard.

### 4.2 Hermes push — Weixin / QQ

- **Files:**
  - `internal/chatlog/hermespush/hermes_weixin.go` — wire protocol, `weixinSessionExpired = -14`.
  - `internal/chatlog/hermespush/hermes_qq.go` — QQ bridge.
  - `internal/chatlog/hermespush/hermes_qq_bridge.py` / `hermes_weixin_bridge.py` — Python side of the bridge, depends on `hermes agent` being installed.
  - `internal/chatlog/http/hermes_weixin.go` / `hermes_qq.go` — HTTP handler side.
- **Why fragile:**
  - Bridges to an external `hermes agent` runtime (Python), so a Python-side update can silently break Go-side push.
  - No test coverage (see §3.1).
  - `WeixinConfig.Token` uses `json:"-"` (line 39), so the token never serializes to JSON. But other fields like `BaseURL` / `CdnBaseURL` do serialize, and the env file path is referenced. If a future change writes the env file to a world-readable location, the token would leak.
  - `weixinSessionExpired = int64(-14)` is hardcoded — if upstream changes the error code, all push will silently fail without retry.
- **Safe modification:** Add integration smoke tests that run against a mock bridge; never change `weixinSessionExpired` without a corresponding test.

### 4.3 MiniMax 5-key pool (`miniMaxAPIKeyPool`)

- **Files:**
  - `internal/chatlog/semantic/client.go` — pool struct, `tryAcquire`, `recordError`, `Snapshot`, `classifyMiniMaxErrorBucket`.
  - `internal/chatlog/semantic/client_test.go` — 4 HA-001 tests + 3 HA-002 tests (per `progress.md`).
- **Why fragile:**
  - Singleton at `var miniMaxGlobalKeyPool = &miniMaxAPIKeyPool{}` — only one pool per process. If `NewClient()` is called multiple times (e.g. for tests, or for the dailyreport vs temporalgraph subsystems), they share state.
  - The pool's internal `slots` slice is populated lazily on the first `tryAcquire`. If `recordError` is called before any `Acquire`, it will crash (per `progress.txt` Codebase Patterns line 13 — this is a known constraint, documented as "recordError can rely on p.slots being populated").
  - The classifier (`classifyMiniMaxErrorBucket`) uses substring match. If MiniMax changes "new_sensitive (1026)" to "policy_violation (1026)", `sensitive_1026` failures will be unclassified and silently re-queued.

### 4.4 Key scanning (macOS WeChat 4.x)

- **Files:**
  - `internal/wechat/key/darwin/scanner_cgo_darwin.go` / `scanner_nocgo_darwin.go` — cgo vs nocgo build paths.
  - `internal/wechat/key/darwin/init_scan.go` — initialization.
  - `internal/wechat/key/darwin/v4.go` — v4 protocol specifics.
  - `cmd/chatlog/cmd_mac_key_helper_darwin.go` — CLI helper.
- **Why fragile:**
  - Two build paths (cgo, nocgo) — divergence risk. `go build ./...` will compile one path; the other may rot.
  - No tests, no fixtures. Changes to the scan loop depend on manual testing on the live macOS environment.
  - Reads WeChat process memory, which is fragile across WeChat version updates. A minor WeChat 4.x release could invalidate the scan offsets.
  - `init_scan.go` references `chatlog-key-tools` (in `.cache/`), suggesting an out-of-tree test fixture.

### 4.5 Ralph auto-merge flow (`scripts/ralph/ralph.py`)

- **Files:**
  - `scripts/ralph/ralph.py` — `ensure_work_branch`, `auto_merge_branch`, `BASE_BRANCH` / `WORK_BRANCH` module-level globals.
  - `scripts/ralph/test_branch_merge.sh` — sandbox regression (mktemp + 3 scenarios).
  - `scripts/ralph/CLAUDE.md` / `VALIDATOR.md` — agent instruction files with "do not execute git checkout / git merge" guardrails.
- **Why fragile:**
  - Module-level globals (`BASE_BRANCH`, `WORK_BRANCH`) reset to `None` across `python3` process boundaries. The sandbox script deliberately uses a single python3 process for the entire `ensure → story_commit → auto_merge` chain; any future maintainer who splits this into multiple processes will see silent no-op (per `progress.txt` Codebase Patterns).
  - `--check` path and live path share `ensure_work_branch` with a `check_only: bool` parameter. If a future change adds a new branch lifecycle primitive, it must respect both paths.
  - `auto_merge_branch` has 4 early-return branches and 1 `sys.exit(1)` branch. The `main()` closure calls `sys.exit(0)` after `auto_merge_branch` returns; a future change that re-orders these could mask a merge failure.
  - Branch lifecycle is "ralph.py only" — `developer agent` and `validator agent` are forbidden from `git checkout` or `git merge`. This is documented in `CLAUDE.md` and `VALIDATOR.md` but relies on the human/agent reading the instructions, not on a runtime guard.

### 4.6 WCDB / WeChat database access

- **Files:**
  - `internal/wechatdb/wcdbapi/client.go` — WCDB-compatible client.
  - `internal/wechatdb/repository/message.go` — message repository (with the `GetContact` FIXME).
- **Why fragile:**
  - Reads WCDB-formatted SQLite databases, which is a fork of upstream SQLite. If the WCDB format changes (e.g. encrypted table layout shifts), reads could fail silently and return empty results.
  - `message.go:112` "群聊用户名称重复" FIXME compounds with the `IsSelf` FIXME — the same underlying identity problem is acknowledged in two places.

---

## 5. Security Considerations

### 5.1 HTTP endpoint surfaces that touch private data

- **Files:** `internal/chatlog/http/route.go` and per-handler files (`graph.go`, `daily_report.go`, `mcp.go`).
- **Risk:** Endpoints that take `keyword` or `time range` and return graph events/facts/relations (`/api/v1/graph/timeline`, `/api/v1/graph/query`) can be used to enumerate private chat content if the HTTP server binds to anything other than `127.0.0.1`. Verify `httpService` listen address (per `progress.md`, the service is bound to `127.0.0.1:5030`, but the configurable default in `internal/chatlog/conf/server.go` should be checked).
- **Mitigation:** Default listen should be `127.0.0.1`. Any `0.0.0.0` binding should require explicit opt-in (already partially enforced by `cmd/chatlog/cmd_http.go`).

### 5.2 Prompt sanitization — partial coverage

- **Files:** `internal/chatlog/temporalgraph/helpers.go` (regexes: `phoneLikeRe`, `wxIDLikeRe`, `rawIDLikeRe`).
- **What's covered:** `talker_id` / `sender_id` removal, speaker name sanitization, participants capped at 24, entity hints capped at 24, context trimmed to 2 before + target + 1 after, content truncated to `promptContentMaxRunes=2400` / `promptContextContentMaxRunes=360`.
- **Gaps:**
  - `phoneLikeRe = regexp.MustCompile(`1[3-9]\d{9}`)` matches 11-digit Chinese mobile numbers. Does not match landlines, international numbers, or 5-digit short numbers (短号) like `10086` or internal `12345`. The 2026-06-04 middleware incident timeline had 短号查询告警 — these short-number entities would survive into the prompt.
  - No sanitization for file paths, URLs, or email addresses in the prompt payload. If a chat message contains `https://internal.crm.example/...` or `foo@bar.com`, these will be sent to the chat provider.
  - Speaker name sanitization is not unit-tested. The exact behavior of "replaces digits / non-CJK" needs a fixture test.

### 5.3 Sensitive error handling — `sensitive_1026` / `sensitive_1027`

- **Files:** `internal/chatlog/semantic/client.go` — non-retryable matcher for MiniMax 1026/1027.
- **Status:** Verified to skip key rotation (per `progress.md` 2026-06-05 evidence). Tests cover the positive case.
- **Concerns:**
  - Bucket classifier is substring match against upstream error text. If MiniMax internationalizes the error text (e.g. Chinese: "内容安全违规 1026"), the classifier would fail to recognize it and re-queue the request, burning quota on a request that will never succeed.
  - The quarantine behavior (`recordError` moves key to quarantine on `minimax_auth_error` only) is verified at the unit-test level for `auth_error` but not for `minimax_rate_limited` — the allow-list says only auth errors should quarantine, but a regression that adds rate-limit to the quarantine list would silently exhaust the key pool.

### 5.4 Logs and config backups retention

- **Files:** `logs/chatlog-alpha-*.log`, `.cache/daily-report-config/chatlog-server.json.bak-*`.
- **Risk:** `logs/` contains HTTP access logs only (per `progress.md` 2026-06-06 — `logs/chatlog-alpha-20260606_090057.log` is `0B`), but if a future change starts logging model request bodies, the log files will contain chat-derived content. The 4 `chatlog-server.json.bak-*` files have 4 different semantic configs but no `api_key` field — verified by code review of `internal/chatlog/conf/semantic.go` (the `api_key` field is in the struct but populated from env, not from the persisted file).
- **Mitigation:** Add a pre-commit hook that greps `logs/` for `sk-` prefixes and `wxid_` patterns. Already enforced indirectly by `.gitignore` (`logs/` is not in `.gitignore` per `ls .gitignore` referenced from commits — verify state).

---

## 6. Performance Bottlenecks

### 6.1 Graph extraction throughput bound by MiniMax quota

- **Symptom:** `processing_rate_per_minute≈11.28` (per `progress.md` 2026-06-08) at `workers=3, enqueue_workers=1`. The 3911-pending requeue drained slowly even with all 5 MiniMax keys available.
- **Files:** `internal/chatlog/temporalgraph/manager.go` — `realtimeMessageScanLimit=100`, `chunkMessageBatchSize=300`, `realtimeSessionScanLimit=200`.
- **Cause:** Each source requires 1-3 MiniMax chat calls (extraction + 3-attempt decode retry). The quota cap is the real bottleneck, not the worker count.
- **Improvement path:**
  - Bump `enqueue_workers` to 4-5 to feed the worker pool faster.
  - Cache extracted facts/relations by `(source_id, content_hash)` to skip reprocessing the same content.
  - Batch multiple sources into a single chat call (multiple messages per prompt), accepting reduced extraction quality in exchange for higher throughput.

### 6.2 `internal/chatlog/http/` `mcp.go` / `middleware.go` — no observability

- **Risk:** Both files are referenced in the file listing but not yet read. They may be the entry points for MCP-style access and could have cold-start latency or blocking I/O paths that aren't visible in the runtime status.

### 6.3 Full `init.sh --full` rarely run

- **Files:** `init.sh` (per `progress.md`, `--full` includes `go test ./...` + `make build`).
- **Status:** `progress.md` repeatedly notes "Not Verified: full gate not run" for documentation-only side tasks and rules refreshes. This is fine for documentation changes but means a Go-level regression could land without `--full` coverage.

---

## 7. Scaling Limits

### 7.1 Single-host SQLite — no replication

- The graph store is a single SQLite file on the user's local disk. The `127.0.0.1:5030` service reads/writes it directly. There is no multi-host, no replication, no remote access. This is by design (local tool) but means:
  - A disk failure loses the graph.
  - Concurrent access from multiple services (e.g. TUI + HTTP + Hermes push all running) competes for the file lock.
  - A user's macOS backup (`Time Machine`) may snapshot the file mid-write, producing a corrupted backup.

### 7.2 5-key pool ceiling

- MiniMax key pool is sized at 5 (per `~/.zshrc` `MINIMAX_API_KEYS` count). If the upstream account needs 6+ keys for higher throughput, the pool saturates. HA-001/002/003 hard-code `configured_key_count=5` semantics; scaling beyond 5 needs new code paths.

### 7.3 Model throughput at `workers=5`

- Per `progress.md`, the optimal throughput was at `workers=5, enqueue_workers=1` with `MiniMax-M2.7` and `chat_max_tokens=4096`. Switching to `M3` with `8192` tokens produced no measurable improvement (per the 2026-06-05 switch evidence). The `adaptiveMaxWorkers` cap is `maxGraphWorkers=12` (per `manager.go:36`), so the configured 5 is well below the cap; the bottleneck is upstream rate-limiting, not local workers.

---

## 8. Dependencies at Risk

### 8.1 `cgo` requirement for normal local build path

- **AGENTS.md note:** "Go `1.24.0`, with cgo required for the normal local build path."
- **Files:** `internal/wechat/key/darwin/scanner_cgo_darwin.go` (cgo path) vs `scanner_nocgo_darwin.go` (nocgo fallback).
- **Risk:** Cgo requires a C toolchain (Xcode CLI tools on macOS, MinGW on Windows). Cross-compilation (`GOOS=linux` from macOS, etc.) is not straightforward. CI on Linux would need the WeChat-specific scanner stubbed out.

### 8.2 `mattn/go-sqlite3` (cgo) vs modernc.org/sqlite (pure Go)

- **Files:** `internal/chatlog/temporalgraph/store.go:13` uses `github.com/mattn/go-sqlite3` (cgo).
- **Risk:** cgo dependency is the same as above. A pure-Go alternative (`modernc.org/sqlite`) would simplify cross-compilation but is a larger refactor.

### 8.3 WeChat 4.x schema assumptions

- **Files:** `internal/model/message_v4.go`, `internal/wechatdb/wcdbapi/client.go`.
- **Risk:** WeChat does not publish a schema for v4. The code is reverse-engineered, and a WeChat 4.x update could change column types, table names, or zstd compression format. The 5-month commit gap (Feb 2026 to June 2026 in `git log`) suggests a stable period, but this is a permanent risk.

---

## 9. Missing Critical Features / Gaps

### 9.1 No automated test for daily report HTML render path

- `internal/chatlog/dailyreport/renderer_test.go` exists (48 lines per commit `8ba9e182`), but the **enhanced** HTML renderer (`skills/chatlog-http-cli/scripts/render-enhanced-daily-html.go`, 652 lines) has no test file. Future refactors of the HTML output could break the Obsidian-readable structure without a test catching it.

### 9.2 No documented runbook for the half-closed SQLite recovery

- The 2026-06-05 20:37 incident required manual tmux respawn. The HA guard (`scripts/chatlog-ha-guard.sh`) now does this automatically, but the user-facing recovery is "stop chatlog-alpha, restart with `serve --config-dir .cache/daily-report-config`". This is not documented in `AGENTS.md` or `README.md` as a first-line recovery step.

### 9.3 No versioned migration plan for `temporal_graph.db` schema

- The schema in `store.go` is created via `CREATE TABLE IF NOT EXISTS`. There is no migration framework (e.g. `golang-migrate` or hand-rolled version table). If a future change adds a new column, the existing DB will silently miss the new column. Currently this hasn't bitten because the schema has been additive, but it is a latent risk.

### 9.4 No CI / continuous integration

- `init.sh` exists as a local quick gate, but there is no GitHub Actions / GitLab CI configuration. `progress.md` references a "release workflow" (`.github/workflows/`) in the commit log, but the latest commits focus on features, not CI hardening. A pull request from an external contributor could land without any automated check.

### 9.5 `archive/` and `reports.backup-*/` grow unbounded

- `archive/2026-06-06-temporal-graph-failed-reprocess-parallelism-2026-06-06`, `archive/2026-06-10-temporal-graph-failed-reprocess-parallelism/`, `archive/2026-06-12-graph-knowledge-digest/` are the three archived PRDs. `reports.backup-20260606_124656/` is the single backup directory. Per `AGENTS.md`, these stay local and are not committed, but there is no automated cleanup. A multi-year history will accumulate.

---

## 10. Operator / Observability Concerns

### 10.1 Log retention and empty logs

- `logs/chatlog-alpha-20260606_090057.log` is `0B` (per `progress.md`). The file is created but no detailed model-failure logs are written. Operator debugging requires re-running reprocesses with manual observation.
- **Fix:** Add `--verbose` / `--log-level=debug` flag that captures MiniMax request/response bodies (with prompt redaction) to `logs/chatlog-alpha-<date>.log` for postmortem analysis.

### 10.2 Status endpoint gap

- `/api/v1/graph/status?format=json` exposes high-level counts but not:
  - Per-worker lease hold time (so an operator can see "worker 3 is holding its key for 8 minutes — likely hung").
  - Per-bucket failure rate over time (so a rate-limit spike is visible as a time series, not a snapshot).
  - Last successful extraction timestamp (so a stale "processed=X" is detectable).
- **Fix:** Extend `Status()` in `manager.go` to include `worker_stats: [{worker_id, current_source_id, acquired_at, ...}]` and `bucket_trend: [{bucket, last_24h_count}]`.

### 10.3 `chatlog-ha-guard.sh` `--loop` mode unverified long-term

- Per `progress.md` 2026-06-07: "Long soak of the HA guard loop was not run. Reason: the immediate incident was recovered; `--loop` can be run in tmux/launchd for continuous guarding." The recommended one-liner is documented in the progress notes, but no nightly / weekly soak evidence exists.

---

## 11. Specific TODOs / Forward Work Already Identified

These are items that have been formally recorded and are awaiting execution.

### 11.1 `TODOS.md` — `TODO-2026-06-10-graph-store-wal`

- Add `?_journal_mode=WAL&_busy_timeout=5000` to `internal/chatlog/temporalgraph/store.go:31`. Blocked on pending queue drain + `temporal_graph.db` backup. See §1.3.

### 11.2 Old US-005 — blocked story

- Per `progress.md`: "US-005 is `blocked=true` (10-min 窗口内 failed/pending 三连零不可达)". This is a graph stability convergence story that requires a 10-minute window of zero failures and zero pending, which hasn't been achievable. Resolution path is not documented — likely "mark as permanently blocked with note" or "lower the threshold to a larger window".

### 11.3 Old US-006 / US-007 — superseded

- Per `progress.md`: "旧 US-006 / US-007 在 `notes` 字段标记 superseded_by(HA-004 / HA-007)而不是删除". The `passes` / `blocked` fields remain at `false / false`, but the `notes` field carries the superseded relationship. Per `session-handoff.md`, "HA-004 / HA-007 跑完后,会话来手动写 `passes=true` + notes 'completed as part of HA-xxx'". This is a manual handoff step, not an automated one.

### 11.4 ralph.py → ralph CLI mapping

- `AGENTS.md` and the `Codebase Patterns` note that "RALPH_AUTO_MERGE=0 是显式 opt-out 关合并的唯一合规路径" but the `auto_merge_branch` early-return for `WORK_BRANCH is None` is silent. A user running `ralph.py` with `prd.json` not having `branchName` will not see "branch management not enabled" until `auto_merge_branch` is reached, and even then only as a single `print` line. A `WARN` at startup if `branchName` is missing would catch this earlier.

### 11.5 `feature_list.json` `harness-assessment-report` — deferred

- Status `deferred`, with `done_criteria` requiring `pre-change 20/100 baseline and post-change validation evidence`. Currently no evidence recorded.

---

## 12. Anti-Patterns Observed

### 12.1 Multiple PRD "re-verify" runs that flip `passes=true` on unchanged code

- Per `scripts/ralph/progress.txt` "re-verify" entries: when `prd.json` is reset, the developer agent runs a minimal sandbox smoke test and flips `passes=true` without re-implementing. This is a documented pattern (per Codebase Patterns line 217 "re-verify 模式"), but it has a subtle risk: if the code drift has been silent (e.g. a helper function changed in an unrelated PR), the smoke test may not catch it. Recommend: re-verify stories should also run `go test ./...` to catch helper-level drift.

### 12.2 `chatlog-server.json.bak-*` files lack a cleanup policy

- The `.cache/daily-report-config/` directory has 4 backup files dating from 2026-06-05 to 2026-06-06. No documented retention policy. A long-running install will accumulate.

### 12.3 `init.sh` quick gate does not run `go test`

- Per `AGENTS.md`: `./init.sh` is the "smallest honest gate first", but it only runs the root harness check + daily report help + HTTP endpoint list. It does NOT run `go test`. Focused tests (`go test ./internal/chatlog/...`) are recommended for Go logic changes, but the quick gate is silent on test regressions.

### 12.4 `reports` symlink in `git status` is a persistent false-positive

- `progress.md` 2026-06-10: "Discovery: `reports` is a symlink to `/Volumes/WorkSSD/Dev/openclaw_mz/knowledge/raw/微信每日聊天记录`; `.gitignore` `reports/` does not match the symlink, so `?? reports` is pre-existing untracked state." The git status will always show `?? reports` regardless of new artifacts. This masks accidental `reports/foo.md` additions in the working tree.

---

## 13. Source of Truth Mapping

When a concern needs to be traced back to authoritative state, consult these files in this order:

1. `progress.md` — current state, evidence, risks, next step.
2. `session-handoff.md` — restartable handoff for the next session.
3. `feature_list.json` — active feature, scope, done criteria, evidence, dependencies.
4. `scripts/ralph/progress.txt` — Ralph iteration log + reusable Codebase Patterns.
5. `scripts/ralph/prd.json` — current PRD story list and per-story passes/notes.
6. `TODOS.md` — open TODO items.
7. `git log --oneline -10` — recent commits for context.
8. `git status` — current working tree state (be aware of pre-existing dirty files).

---

*Concerns audit: 2026-06-15*
