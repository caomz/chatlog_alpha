<!-- refreshed: 2026-06-15 -->
# Architecture

**Analysis Date:** 2026-06-15

## System Overview

`chatlog_alpha` is a Go local-tool product (Go 1.24.0, cgo required) for WeChat 4.x desktop data access on macOS and Windows. The system is organized as a thin CLI/TUI shell that owns application lifecycle and routes work to a small set of cooperating services, each backed by either a decrypted SQLite/WeChatDB layer or an optional LLM provider.

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│                          CLI / TUI Shell (`main.go`, `cmd/chatlog/`)         │
│   `root.go` `cmd_http.go` `cmd_report.go` `cmd_bench.go` `cmd_serve.go`      │
│   `cmd_mac_key_helper_darwin.go`                                             │
└──────────────────────┬───────────────────────────────────────────────────────┘
                       │ builds Manager
                       ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│        Application Manager (`internal/chatlog/manager.go`, `app.go`)         │
│   `ctx.Context` (account, keys, workdir, http state)                         │
│   `database.Service`  `http.Service`  `chatlog/wechat.Service`                │
│   `App` (tview-based TUI: menu / form / infobar / footer)                    │
└────┬─────────────────────┬─────────────────────┬─────────────────────────────┘
     │                     │                     │
     ▼                     ▼                     ▼
┌─────────────────┐  ┌────────────────────┐  ┌────────────────────────────────┐
│ Data Access     │  │ HTTP / MCP Service │  │ Optional LLM/Vector Layer      │
│ `wechatdb/`     │  │ `chatlog/http/`    │  │ `chatlog/semantic/`            │
│ `wechat/`       │  │ gin router         │  │ `chatlog/temporalgraph/`       │
│ decryptor + key │  │ static / wasm      │  │ `chatlog/hermespush/`          │
│ `wechatdb/wcdb` │  │ MCP SSE/streamable │  │ `chatlog/dailyreport/`         │
│ repo layer      │  │ message hook       │  │ `chatlog/messagehook/`         │
└─────────────────┘  └────────────────────┘  └────────────────────────────────┘
                                                       │
                                                       ▼
                                ┌──────────────────────────────────────┐
                                │ Persistence / Output                 │
                                │ SQLite (mattn/go-sqlite3)            │
                                │ work dir dec .dat/.jpg/.silk          │
                                │ `reports/` HTML/MD/JSON              │
                                │ `~/.chatlog_graph/temporal_graph.db` │
                                └──────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| `main()` | Entry point; calls `chatlog.Execute()`. | `main.go` |
| `Execute()` / `rootCmd` | Cobra root; resolves TUI vs subcommand flow. | `cmd/chatlog/root.go` |
| `cmd http` | CLI client over the embedded HTTP API (list/call). | `cmd/chatlog/cmd_http.go` |
| `cmd serve` | Hidden subcommand for headless server bootstrap. | `cmd/chatlog/cmd_serve.go` |
| `cmd report` | Daily and graph-digest report generation; calls HTTP service. | `cmd/chatlog/cmd_report.go` |
| `cmd bench` | Provider/model routing benchmark (dry-run by default). | `cmd/chatlog/cmd_bench.go` |
| `cmd mac-key-helper` | Privileged macOS key scan via `osascript` elevation. | `cmd/chatlog/cmd_mac_key_helper_darwin.go` |
| `Manager` | Lifecycle, restart, key acquisition, account switching. | `internal/chatlog/manager.go` |
| `App` | TUI shell (tview/tcell). | `internal/chatlog/app.go` |
| `ctx.Context` | TUI-mode account/key/hook/semantic state, history. | `internal/chatlog/ctx/context.go` |
| `database.Service` | Wraps `wechatdb.DB`; lifecycle (init/decrypting/ready/error), message-hook notifier. | `internal/chatlog/database/service.go` |
| `http.Service` | gin router, MCP server, hook event bus, semantic incremental watcher, semantic/graph managers. | `internal/chatlog/http/service.go`, `internal/chatlog/http/route.go` |
| `chatlog/wechat.Service` | Decryption, WAL monitoring, auto-decrypt. | `internal/chatlog/wechat/service.go` |
| `wechat.Manager` | Process detection, account discovery. | `internal/wechat/manager.go` |
| `wechatdecrypt` | Platform decryptors (v3/v4) for db files. | `internal/wechat/decrypt/...` |
| `wechatkey` | Memory/key extraction (darwin/windows). | `internal/wechat/key/...` |
| `wechatprocess` | WeChat process detection per platform. | `internal/wechat/process/...` |
| `wechatdb.DB` | Repository facade. | `internal/wechatdb/wechatdb.go` |
| `datasource` | SQLite/WCDB-backed data source. | `internal/wechatdb/datasource/datasource.go` |
| `wcdbapi` | WCDB-compatible client. | `internal/wechatdb/wcdbapi/client.go` |
| `repository` | Domain accessors (message/contact/chatroom/media/session). | `internal/wechatdb/repository/...` |
| `semantic.Manager` | Embedding/Chat/Rerank client manager, MiniMax key pool, indexing. | `internal/chatlog/semantic/manager.go`, `client.go`, `store.go` |
| `temporalgraph.Manager` | Source queue, entities, facts, events, relations, digest. | `internal/chatlog/temporalgraph/manager.go`, `store.go`, `digest.go` |
| `messagehook.Service` | Keyword-driven event delivery to MCP/Hermes/HTTP/POST. | `internal/chatlog/messagehook/service.go` |
| `hermespush` | Weixin/QQ Hermes bridge for outbound push. | `internal/chatlog/hermespush/...` |
| `dailyreport` | Mention-based daily report generator. | `internal/chatlog/dailyreport/...` |
| `ui/*` | TUI components (menu, footer, infobar, form, help). | `internal/ui/...` |
| `pkg/config` | Viper-backed config manager. | `pkg/config/...` |
| `pkg/process` | Single-instance PID lock. | `pkg/process/process.go` |
| `pkg/util` | Shared helpers: `dat2img`, `silk`, `zstd`, time/strings. | `pkg/util/...` |

## Pattern Overview

**Overall:** Layered service-oriented monolith with explicit dependency boundaries; all wiring happens in the `Manager` (TUI) or in `cmd_serve.go` (headless), and every long-running service exposes a `Start()`/`Stop()` pair so it can be torn down on account switch.

**Key Characteristics:**

- **Service composition, not frameworks:** A `Manager` holds `db`, `http`, `wechat` services plus an `App`; `internal/chatlog` packages are the only ones allowed to depend on multiple subsystems.
- **Config interfaces:** `database.Config`, `chatlog/http.Config`, `wechat.Config`, `semantic.ConfigProvider` are tiny interfaces the services consume, decoupling them from the `conf` package and from the TUI `ctx` runtime. `ServerConfig` and `TUIConfig` both implement these getters (`internal/chatlog/conf/server.go`, `internal/chatlog/conf/tui.go`).
- **Single-instance + lifecycle ownership:** The TUI root grabs a PID lock (`pkg/process/process.go`), runs the manager, and blocks on the tview app. The headless `serve` command uses the same `Manager.CommandHTTPServer`.
- **WAL-aware hot-path:** Auto-decrypt watches WAL files (`internal/chatlog/wechat/service.go`) and re-issues decrypt events; the HTTP service watches session NOrder to trigger semantic incremental indexing (`internal/chatlog/http/service.go`).
- **Privacy by default:** HTTP responses are JSON; `format=json` is the verification mode. The MiniMax/MiniMax key pool endpoint is privacy-safe (`internal/chatlog/http/route.go`). Private chat-derived content is never printed by tests/CLI checks.

## Layers

**Entry Layer (`main.go`, `cmd/chatlog/`):**
- Purpose: parse CLI args, route to manager flow, drive daily report / HTTP CLI / bench / serve.
- Location: `main.go`, `cmd/chatlog/`
- Contains: Cobra commands, subcommand wiring, and short-lived worker entry points.
- Depends on: `internal/chatlog`, `internal/chatlog/conf`, `internal/chatlog/dailyreport`, `internal/chatlog/semantic`, `internal/chatlog/temporalgraph`, `internal/wechatdb`, `pkg/process`.
- Used by: end users (`chatlog ...`); headless service runner.

**Application Layer (`internal/chatlog/manager.go`, `app.go`):**
- Purpose: own service lifecycle, TUI shell, and key acquisition.
- Location: `internal/chatlog/`
- Contains: `Manager`, `App` (tview), `ctx.Context` for TUI state.
- Depends on: every other internal service.
- Used by: `cmd/chatlog/root.go`, `cmd/chatlog/cmd_serve.go`.

**Configuration Layer (`internal/chatlog/conf/`, `pkg/config/`):**
- Purpose: typed configs (`TUIConfig`, `ServerConfig`, `MessageHook`, `SemanticConfig`), viper-backed loading, defaults, env override.
- Location: `internal/chatlog/conf/{conf,server,tui,message_hook,semantic}.go`, `pkg/config/{config,default,types}.go`.
- Contains: viper manager wrappers, default maps, hook/semantic config normalization.
- Depends on: `pkg/config`, `internal/wechat` (only for TUI history parsing).
- Used by: every service that needs a `Config` interface.

**Data Layer (`internal/wechatdb/`, `internal/wechat/`):**
- Purpose: platform-specific WeChat process detection, key extraction, decryption, and WCDB-compatible data access.
- Location: `internal/wechatdb/{wechatdb.go,datasource/,repository/,wcdbapi/}`, `internal/wechat/{wechat.go,manager.go,decrypt/,key/,process/,model/}`.
- Contains: per-platform build-tagged code (darwin/windows), repository methods over SQLite/WCDB.
- Depends on: `mattn/go-sqlite3`, `gopsutil`, `fsnotify`.
- Used by: `database.Service`, `chatlog/wechat.Service`, `dailyreport`, `semantic` (read-only).

**HTTP / MCP Layer (`internal/chatlog/http/`):**
- Purpose: gin router, embedded static/wasm, MCP (SSE + streamable) server, message-hook event delivery, semantic/graph handlers, daily-report handler.
- Location: `internal/chatlog/http/{service,route,mcp,middleware,daily_report,graph,semantic_qa,hermes_qq,hermes_weixin,sns_media,static,wasm}.go`.
- Contains: handlers grouped by domain in `route.go`, plus per-domain files.
- Depends on: `database.Service`, `semantic.Manager`, `temporalgraph.Manager`, `messagehook`, `hermespush`.
- Used by: external HTTP clients, embedded `index.htm`, MCP-compatible agents.

**Optional Service Layer (`internal/chatlog/semantic`, `temporalgraph`, `hermespush`, `dailyreport`, `messagehook`):**
- Purpose: feature-flagged subsystems that attach to the HTTP service if initialization succeeds.
- Location: `internal/chatlog/semantic/...`, `internal/chatlog/temporalgraph/...`, `internal/chatlog/hermespush/...`, `internal/chatlog/dailyreport/...`, `internal/chatlog/messagehook/...`.
- Contains: per-feature managers, stores, types; LLM client + key pool; graph digest CLI/HTTP path.
- Depends on: `database.Service`, `wechatdb`, `conf`, `model`, optional `hermespush` (Python bridge scripts under `hermespush/`).
- Used by: HTTP handlers, `cmd report daily|graph|bench`, `messagehook` delivery.

**TUI Layer (`internal/ui/`):**
- Purpose: tview components used by `App`.
- Location: `internal/ui/{footer,form,infobar,menu,help,style}/`.
- Contains: small, focused widgets.
- Depends on: `tview`, `tcell`, `ctx.Context`, `Manager`.
- Used by: `internal/chatlog/app.go`.

**Shared Utilities (`pkg/`):**
- Purpose: cross-cutting helpers.
- Location: `pkg/{config,process,util/{dat2img,silk,zstd},filemonitor,filecopy,version,appver}/`.
- Contains: viper wrapper, single-instance lock, image/silk/zstd codecs, version, file monitor.
- Depends on: third-party libs.
- Used by: services and CLI.

## Data Flow

### Primary Request Path (HTTP API)

1. `main()` → `chatlog.Execute()` → `rootCmd` → `Manager.New()` then `Manager.Run("")` (`cmd/chatlog/root.go`, `internal/chatlog/manager.go`).
2. `Manager.Run` creates `ctx.Context`, then wires `wechat.Service`, `database.Service`, `http.Service` and starts HTTP+DB (`internal/chatlog/manager.go:45-74`).
3. HTTP request → gin router in `internal/chatlog/http/route.go` → `checkDBStateMiddleware` (`internal/chatlog/http/middleware.go`) → handler (`handleSessionsCompat`, `handleHistory`, `handleGraphStatus`, etc.).
4. Handler calls `s.db` (database.Service) for messages/sessions/contacts; for graph/semantic it delegates to `s.graph` or `s.semantic` (`internal/chatlog/http/service.go:122-133`).
5. `database.Service.Start` constructs `wechatdb.DB` from `WorkDir` or `DataDir` (v4) and runs the `repository` layer (`internal/chatlog/database/service.go:53-69`).
6. Repository executes via `datasource.DataSource` (SQLite or wcdb) and returns `model.Message` etc. (`internal/wechatdb/datasource/datasource.go`).
7. Response is JSON; routes that take `format` support `text` mode.

### Headless Server Path (cmd serve)

1. `chatlog serve` → `Manager.CommandHTTPServer(configDir, nil)` (`cmd/chatlog/cmd_serve.go`).
2. Manager loads `ServerConfig` via `conf.LoadServiceConfig`; resolves `DataDir`/`WorkDir`/`DataKey`/`ImgKey`.
3. Background goroutine: decrypts DB files if `WorkDir` is empty, then calls `db.Start()`. (`internal/chatlog/manager.go:830-901`)
4. `http.ListenAndServe` blocks; semantic/graph managers watch incrementally.

### Daily Report Path

1. `chatlog report daily` → `runReportDaily` (`cmd/chatlog/cmd_report.go`).
2. Loads TUI config, opens `wechatdb.DB` (or hits `chatlog http list` if `--http` is set), and calls `dailyreport.GenerateDailyReport` (`internal/chatlog/dailyreport/service.go`).
3. Optionally `applyDailyAIAnalysis` (summary/vision) via `semantic.Client` (`internal/chatlog/http/daily_report.go`).
4. Renders Markdown / JSON to `reports/`.

### Graph Digest Path

1. `chatlog report graph` → `runReportGraph` (`cmd/chatlog/cmd_report.go`).
2. Calls `POST /api/v1/graph/digest` on the running HTTP service with `start`/`end`/`summary` flags.
3. `temporalgraph.Manager` aggregates from `Store` (entities, facts, events, relations) and writes `reports/graph-digest-<start>_<end>.md` via the HTTP handler (`internal/chatlog/http/graph.go`, `internal/chatlog/temporalgraph/digest.go`).
4. Verification is by path/size/section-count only; body never printed.

### Message Hook Path

1. `database.Service.initMessageHook` polls the DB for keyword matches and posts `messagehook.Event` to the notifier.
2. Notifier is set by `http.Service.pushMessageHookEvent`, which appends to in-memory ring, persists to `chatlog_hook_events.json`, broadcasts over SSE, and forwards via `hermespush` or HTTP POST.

**State Management:**

- TUI mode: `ctx.Context` holds the live mutable state (current account, keys, hooks) and serializes via `sync.RWMutex`. `ctx.Refresh` and `ctx.UpdateConfig` round-trip into the TUI config.
- Headless mode: `ServerConfig` is read once at startup; `conf.SetHook*` / `SetSemanticConfig` mutators persist through the same `config.Manager`.
- In-memory caches inside `http.Service`: `md5PathCache`, `snsMediaKeyCache`, `hookEvents` (capped 200), `statsCache` (TTL 60s default), semantic `LastSessionN`.

## Key Abstractions

**`datasource.DataSource` (`internal/wechatdb/datasource/datasource.go`):**
- Purpose: abstract access to messages/contacts/chatrooms/sessions/SNS media, with fsnotify callback and ad-hoc SQL.
- Examples: `internal/wechatdb/datasource/datasource.go`, `internal/wechatdb/datasource/wcdb/`.
- Pattern: interface; SQLite path for v3 and WCDB-compatible path for v4. Repository is the only consumer.

**`wechat.Account` (`internal/wechat/wechat.go`):**
- Purpose: one detected WeChat process and its derived keys, used by both the TUI and the headless service.
- Examples: `internal/wechat/wechat.go`, `internal/chatlog/manager.go`.
- Pattern: domain object with `GetKey(ctx)` that fans out to platform-specific scanners (`internal/wechat/key/darwin`, `internal/wechat/key/windows`).

**`ctx.Context` (`internal/chatlog/ctx/context.go`):**
- Purpose: TUI runtime state, history parser, account switcher, config updater.
- Examples: `internal/chatlog/ctx/context.go`.
- Pattern: aggregate root with mutex-guarded mutators; consumed by `Manager`, `App`, and `chatlog/wechat.Service`.

**`http.Service` (`internal/chatlog/http/service.go`):**
- Purpose: cross-feature service container (router, MCP, hook bus, semantic watcher).
- Examples: `internal/chatlog/http/service.go`.
- Pattern: facade over many subsystems; instantiated by `NewService` and started by `Manager`.

**`temporalgraph.Manager` (`internal/chatlog/temporalgraph/manager.go`):**
- Purpose: source queue, entity/fact/event/relation store, digest aggregator, failure bucketing.
- Examples: `internal/chatlog/temporalgraph/manager.go`, `store.go`, `digest.go`, `digest_markdown.go`, `adaptive.go`, `buckets_test.go`.
- Pattern: long-running manager with explicit pause/resume/rebuild; failures bucketed (rate_limited/timeout) before any requeue.

**`semantic.Manager` (`internal/chatlog/semantic/manager.go`):**
- Purpose: chat/embedding/rerank client management, MiniMax key pool, indexing, search routing.
- Examples: `internal/chatlog/semantic/manager.go`, `client.go`, `store.go`.
- Pattern: provider-aware manager that hot-swaps providers via `conf.NormalizeSemanticConfig` and runs `Incremental`/`Rebuild` jobs.

## Entry Points

**TUI (default `chatlog` invocation):**
- Location: `main.go` → `cmd/chatlog.Execute()` → `rootCmd` → `Manager.Run("")` (`cmd/chatlog/root.go:41-49`).
- Triggers: launching the binary with no args (or with `serve`/`http`/`report`/`bench`).
- Responsibilities: PID lock, service wiring, blocking on tview app.

**Headless Server:**
- Location: `cmd/chatlog/cmd_serve.go`.
- Triggers: `chatlog serve --config-dir <dir>` (hidden).
- Responsibilities: load `ServerConfig`, run decryption if needed, block on `http.ListenAndServe`.

**HTTP CLI:**
- Location: `cmd/chatlog/cmd_http.go`.
- Triggers: `chatlog http list` and `chatlog http call ...`.
- Responsibilities: name resolution to a gin endpoint, query/header/path-param templating, optional body, response output.

**Daily Report CLI:**
- Location: `cmd/chatlog/cmd_report.go::runReportDaily`.
- Triggers: `chatlog report daily ...`.
- Responsibilities: opens DB (or calls HTTP), generates report, writes Markdown/JSON.

**Graph Digest CLI:**
- Location: `cmd/chatlog/cmd_report.go::runReportGraph`.
- Triggers: `chatlog report graph --days 7`.
- Responsibilities: invokes `POST /api/v1/graph/digest` on the running HTTP service.

**Benchmark CLI:**
- Location: `cmd/chatlog/cmd_bench.go`.
- Triggers: `chatlog bench ...` (dry-run by default; `--execute` calls upstream).
- Responsibilities: provider/model routing bench; emits summary or JSON.

**Privileged macOS Key Helper:**
- Location: `cmd/chatlog/cmd_mac_key_helper_darwin.go` (build tag `darwin`).
- Triggers: invoked via `osascript` with administrator privileges by `Manager.tryDarwinPrivilegedKeyHelper`.
- Responsibilities: scans the target WeChat process and returns the 64-hex data key.

## Architectural Constraints

- **Threading:** TUI main goroutine is owned by tview; HTTP server runs in its own goroutine inside `http.Service.Start`; `database.Service.initMessageHook` runs its own polling loop; semantic incremental watcher runs as a `time.Ticker` in a dedicated goroutine (`internal/chatlog/http/service.go:200-284`). macOS key acquisition launches `osascript` synchronously inside `RestartAndGetDataKey`.
- **Global state:** `wechat.DefaultManager` is a package-level singleton (`internal/wechat/manager.go:12-17`) initialized at package load; the only place the singleton is used is inside the `wechat` package. `ollamaScheduler` and `miniMaxGlobalKeyPool` are package-level singletons inside `semantic` (`internal/chatlog/semantic/client.go:45-47`).
- **Circular imports:** Avoided by routing all cross-package type use through the `internal/chatlog` package or through `conf.Config` interfaces. `temporalgraph` and `semantic` both depend on `chatlog/conf` and `chatlog/database` rather than on `chatlog/http`.
- **Cgo required:** `mattn/go-sqlite3` is imported; build paths and `init.sh` assume a working C toolchain (`go.mod:14`).
- **Privacy boundary:** Tools MUST NOT print private chat-derived content. The `format=json` mode is the contract; the daily report and graph digest are private outputs.

## Anti-Patterns

### Conflating Account Switch with Server Lifecycle

**What happens:** Some TUI flows call `Switch(info, history)` and assume the HTTP service is already torn down (`internal/chatlog/manager.go:76-96`). The HTTP service stop is gated by `HTTPEnabled` and may race with the key acquisition goroutine started in `Manager.Run`.
**Why it's wrong:** Toggling accounts can leak state across accounts if the `database.Service` is reused without `Stop`/`Start`; auto-decrypt's error handler relies on `m.app.QueueUpdateDraw` which may be nil.
**Do this instead:** Treat the service start/stop as transactional (`Manager.StartService`/`Manager.stopService`) and gate every account switch behind them. The `Run` path is the canonical ordering: stop HTTP, stop DB, mutate ctx, start DB, start HTTP.

### Reusing Cobra flag pointers for TUI and CLI flows

**What happens:** TUI and CLI share globals like `Debug` in `cmd/chatlog/root.go:18`; the TUI `Run` reads from `ctx.Context` while CLI handlers use the same viper-backed config.
**Why it's wrong:** A persisted config value can be overwritten by a stale global; the `Manager.SetHTTPAddr` rewrite is a frequent footgun (`internal/chatlog/manager.go:153-166`).
**Do this instead:** For new commands, keep flag binding local to the command file; only mutate runtime state via `ctx.Context` setters and persist through `ctx.UpdateConfig()`.

### Importing `temporalgraph` from `http` to access both managers

**What happens:** `http.Service` constructs both `semantic.Manager` and `temporalgraph.Manager` in `NewService` (`internal/chatlog/http/service.go:122-133`) and exposes them as private fields. Handlers reach for them with a `requireGraph`/`requireSemantic` helper.
**Why it's wrong:** Tests that want to assert handler behavior need to construct a real `Service`; private fields prevent stubbing.
**Do this instead:** Pass `*semantic.Manager` and `*temporalgraph.Manager` through `NewService` and expose `WithSemantic`/`WithGraph` setter helpers used only by tests.

## Error Handling

**Strategy:** `internal/errors` exposes middleware (`RecoveryMiddleware`, `ErrorHandlerMiddleware`) used by the gin router (`internal/chatlog/http/service.go:105-110`). Handlers call `errors.Err(c, ...)` and `errors.InvalidArg(...)`. Service errors bubble up; the `Manager` logs via `zerolog` and surfaces user-facing errors through the TUI `App.showError`.

**Patterns:**

- **Defensive normalization:** `conf.NormalizeSemanticConfig` (`internal/chatlog/conf/semantic.go`) and `conf.CanonicalHookNotifyMode` keep config valid before persisting.
- **Bucket before requeue:** Temporal graph failure handling uses `bucketFromError` and the `keyHealthTracker` allow-list (`internal/chatlog/temporalgraph/manager.go:50-74`) instead of blind requeue.
- **Retryable key errors:** `Manager.isRetryableKeyErr` enumerates macOS-specific transient strings (`internal/chatlog/manager.go:434-461`).
- **Privacy-safe failure responses:** `/api/v1/semantic/mmx/status` returns buckets and counts, never raw keys (`internal/chatlog/http/route.go:383-429`).
- **Graceful shutdown:** `http.Service.Stop` uses a 2-second `context.WithTimeout` for `server.Shutdown` and stops the semantic watcher first (`internal/chatlog/http/service.go:174-198`).

## Cross-Cutting Concerns

**Logging:** `zerolog` is the project logger (`github.com/rs/zerolog`). The CLI sets `log.LstdFlags | log.Lshortfile` in `main.go`; the gin router uses `gin.LoggerWithWriter(log.Logger, "/health")` to skip health probes.

**Validation:** Inputs are validated at the handler boundary using typed request structs (`hookConfigReq`, `semanticConfigReq`, `dailyReportSaveReq`) and `c.ShouldBindJSON`. Cross-package rules live in `conf.ParseHookNotifyTargets` and `conf.ValidateSemanticPlan`-style helpers.

**Authentication:** No traditional user auth. Authorization is **process-local** (PID lock + TUI config directory) and **account-bound** (the WeChat process and its data key). The HTTP server is intentionally a local tool — there is no token model.

**Quotas / privacy boundary:** Model providers (Ollama/DeepSeek/GLM/MiniMax) and Hermes push endpoints are configured by the user. `chatlog report daily --vision` and `--summary` consume quota; the `chatlog report graph --summary` makes at most one model call. The semantic manager exposes `/api/v1/semantic/mmx/status` for the operator/HA guard.

---

*Architecture analysis: 2026-06-15*
