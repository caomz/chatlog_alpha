# Codebase Structure

**Analysis Date:** 2026-06-15

## Directory Layout

```
chatlog_alpha/
├── main.go                          # Entry point; delegates to cmd/chatlog.Execute
├── go.mod / go.sum                  # Go 1.24.0 module file (cgo required)
├── Makefile                         # Build shortcuts
├── init.sh                          # Verification entrypoint (./init.sh, --full, --runtime)
├── README.md / AGENTS.md / CLAUDE.md
├── feature_list.json / progress.md / session-handoff.md
│
├── cmd/
│   └── chatlog/                     # Cobra commands
│       ├── root.go                  # rootCmd, TUI default, single-instance
│       ├── cmd_http.go              # `chatlog http list|call` (CLI over the embedded API)
│       ├── cmd_serve.go             # `chatlog serve` (hidden headless server)
│       ├── cmd_report.go            # `chatlog report daily|graph`
│       ├── cmd_bench.go             # `chatlog bench` (provider/model routing bench)
│       ├── cmd_mac_key_helper_darwin.go  # `chatlog mac-key-helper` (privileged macOS scanner)
│       └── log.go                   # Shared logging setup
│
├── internal/
│   ├── chatlog/                     # Application layer (services, TUI shell)
│   │   ├── app.go                   # TUI App (tview) - menu, footer, form, infobar
│   │   ├── manager.go               # Manager: lifecycle, key acquisition, account switch
│   │   ├── conf/                    # Typed configs: TUI/Server/MessageHook/Semantic
│   │   ├── ctx/                     # TUI runtime state (account, keys, hooks, history)
│   │   ├── database/                # database.Service (wechatdb wrapper + message hook notifier)
│   │   ├── http/                    # gin router, MCP, semantic/graph/daily report handlers
│   │   │   ├── static/index.htm     # Embedded static UI
│   │   │   └── wasm/                # Embedded WASM (keystream helper, video decode)
│   │   ├── dailyreport/             # Mention-based daily report generator
│   │   ├── semantic/                # Embedding/Chat/Rerank manager, MiniMax key pool
│   │   ├── temporalgraph/           # Source queue, entities/facts/events, digest
│   │   ├── hermespush/              # Weixin/QQ Hermes bridge (.go + .py scripts)
│   │   ├── messagehook/             # Keyword event delivery to MCP/Hermes/HTTP/POST
│   │   └── wechat/                  # Decryption + WAL monitor + auto-decrypt
│   │
│   ├── wechat/                      # WeChat process/key/decrypt
│   │   ├── wechat.go                # Account type
│   │   ├── manager.go               # WeChat DefaultManager (process discovery)
│   │   ├── decrypt/                 # Decryptors (common/, darwin/, windows/, validator)
│   │   ├── key/                     # Key extraction (darwin/, windows/, extractor)
│   │   ├── process/                 # Process detection (darwin/, windows/, detector)
│   │   └── model/                   # Process/Status models
│   │
│   ├── wechatdb/                    # WCDB-compatible data access
│   │   ├── wechatdb.go              # DB facade (GetMessages, GetContacts, etc.)
│   │   ├── datasource/              # DataSource interface + SQLite/WCDB implementations
│   │   │   ├── datasource.go
│   │   │   └── wcdb/                # v4 WCDB-compatible datasource
│   │   ├── repository/              # Domain methods: chatroom/contact/media/message/session
│   │   └── wcdbapi/                 # WCDB client (cgo, v4)
│   │
│   ├── model/                       # Domain models + generated protobuf (wxproto/)
│   │   ├── message.go message_v4.go
│   │   ├── contact.go contact_v4.go
│   │   ├── chatroom.go chatroom_v4.go
│   │   ├── session.go session_v4.go
│   │   ├── media.go media_v4.go mediamessage.go sns.go
│   │   └── wxproto/                 # Generated .pb.go types
│   │
│   ├── errors/                      # API error helpers + gin middleware
│   │
│   └── ui/                          # TUI components (tview)
│       ├── footer/  form/  help/  infobar/  menu/  style/
│
├── pkg/                             # Shared utilities (no internal/ deps)
│   ├── config/                      # Viper-backed config manager
│   ├── process/                     # PID-lock single-instance helper
│   ├── util/                        # os/strings/time + dat2img/ + silk/ + zstd/
│   ├── filemonitor/  filecopy/  version/  appver/
│
├── docs/                            # Task-level documentation
│   ├── daily-report.md
│   └── graph-digest.md
│
├── skills/
│   └── chatlog-http-cli/            # Repo-local Codex/Claude skill (SKILL.md + scripts)
│
├── .agents/                         # Project-local Codex skills
│   ├── skills/                      # prime, plan-feature, create-rules, ralph, prd, ...
│   └── commands/                    # Legacy project commands (compat only)
│
├── .claude/                         # Claude-compatible adapters
│   ├── skills/                      # ralph, prd, agent-browser-skill
│   └── commands/
│
├── .codex/                          # Codex hooks
├── .github/workflows/               # CI (release/test)
│
├── scripts/                         # Automation scripts
│   ├── check-root-harness.mjs       # Root harness integrity check
│   ├── chatlog-ha-guard.sh          # HA guard
│   └── ralph/                       # PRD -> story -> developer -> validator loop
│       ├── prd.json progress.txt
│       ├── ralph.py validate.sh test_branch_merge.sh
│
├── tasks/                           # Task snapshots (work artifacts)
├── archive/                         # Date-stamped task archives
├── reports/                         # Symlink to daily reports output dir (private)
├── reports.backup-*/                # Local backups (do not commit)
├── outputs/  logs/  .cache/         # Generated/runtime outputs (do not commit)
└── bin/                             # Built binaries (do not commit)
```

## Directory Purposes

**`cmd/chatlog/`:**
- Purpose: CLI surface; one file per command family, all using Cobra.
- Contains: cobra command vars, flag definitions, Run functions, plus a tiny `log.go` for shared initialization.
- Key files: `root.go`, `cmd_http.go`, `cmd_serve.go`, `cmd_report.go`, `cmd_bench.go`, `cmd_mac_key_helper_darwin.go` (build tag `darwin`).

**`internal/chatlog/`:**
- Purpose: the application layer; everything else (HTTP, semantic, graph, daily report, hermes push, message hook, WeChat glue) is a subpackage of this one.
- Contains: `Manager`, `App`, plus the `conf/`, `ctx/`, `database/`, `http/`, `dailyreport/`, `semantic/`, `temporalgraph/`, `hermespush/`, `messagehook/`, `wechat/` subpackages.
- Key files: `manager.go`, `app.go`, `http/route.go`, `http/service.go`, `http/mcp.go`, `temporalgraph/manager.go`, `semantic/manager.go`, `dailyreport/service.go`.

**`internal/wechat/`:**
- Purpose: cross-platform WeChat process detection, key extraction, and database decryption.
- Contains: account/manager, platform-specific subpackages gated by build tags.
- Key files: `wechat.go`, `manager.go`, `decrypt/decryptor.go`, `key/extractor.go`, `process/detector.go`.

**`internal/wechatdb/`:**
- Purpose: WCDB-compatible data access on top of SQLite and the v4 WCDB client.
- Contains: `DB` facade, datasource interface + impls, repository, wcdbapi cgo client.
- Key files: `wechatdb.go`, `datasource/datasource.go`, `repository/*.go`, `wcdbapi/client.go`.

**`internal/model/`:**
- Purpose: domain structs (Message, Contact, ChatRoom, Session, Media, SNS) and generated protobuf.
- Contains: `_v4.go` siblings for v4 fields, plus `wxproto/` for generated `.pb.go`.
- Key files: `message.go`, `message_v4.go`, `contact.go`, `chatroom.go`, `session.go`, `media.go`, `mediamessage.go`, `sns.go`.

**`internal/errors/`:**
- Purpose: error helper API + gin middleware.
- Contains: `Err`, `InvalidArg`, `RecoveryMiddleware`, `ErrorHandlerMiddleware`.

**`internal/ui/`:**
- Purpose: tview widgets used by the TUI shell.
- Contains: footer, form, help, infobar, menu, style subpackages.
- Key files: `app.go` (in `internal/chatlog`) wires these via `tview.Flex` and `tview.Pages`.

**`pkg/`:**
- Purpose: leaf utilities usable by any package (no `internal/` deps).
- Contains: config wrapper, PID lock, time/strings/os helpers, image/silk/zstd codecs, file monitor, file copy, version, appver.
- Key files: `config/config.go`, `process/process.go`, `util/dat2img`, `util/silk`, `util/zstd`.

**`docs/`:**
- Purpose: behavior-level task documentation.
- Contains: `daily-report.md`, `graph-digest.md`.

**`skills/chatlog-http-cli/`:**
- Purpose: repo-local Codex/Claude skill for CLI/HTTP verification.
- Contains: `SKILL.md` and `scripts/check-harness-skill.mjs`.

**`.agents/`, `.claude/`, `.codex/`:**
- Purpose: harness/adapter assets (skills and command stubs).
- Contains: project-local skills and legacy command shortcuts.

**`scripts/`:**
- Purpose: automation scripts (Node + Python + shell).
- Contains: `check-root-harness.mjs`, `chatlog-ha-guard.sh`, `ralph/` (PRD-driven loop).

## Key File Locations

**Entry Points:**
- `main.go` — calls `chatlog.Execute()`.
- `cmd/chatlog/root.go` — TUI default; `Manager.Run("")` then blocks on tview.
- `cmd/chatlog/cmd_serve.go` — hidden `serve` command, headless HTTP bootstrap.
- `cmd/chatlog/cmd_http.go` — `chatlog http list|call` CLI client.
- `cmd/chatlog/cmd_report.go` — `chatlog report daily|graph`.
- `cmd/chatlog/cmd_bench.go` — `chatlog bench`.
- `cmd/chatlog/cmd_mac_key_helper_darwin.go` — privileged macOS helper.

**Configuration:**
- `pkg/config/config.go` — Viper wrapper.
- `internal/chatlog/conf/conf.go` — loaders: `LoadTUIConfig`, `LoadServiceConfig`.
- `internal/chatlog/conf/server.go` — `ServerConfig` + getters + defaults.
- `internal/chatlog/conf/tui.go` — `TUIConfig`, `ProcessConfig`, `ParseHistory`.
- `internal/chatlog/conf/message_hook.go` — `MessageHook` and notify-mode helpers.
- `internal/chatlog/conf/semantic.go` — `SemanticConfig` + normalization.

**Core Logic:**
- `internal/chatlog/manager.go` — Manager (lifecycle, key acquisition, account switch, run, decrypt).
- `internal/chatlog/app.go` — TUI shell (`App`).
- `internal/chatlog/ctx/context.go` — runtime state.
- `internal/chatlog/database/service.go` — DB service wrapper + state machine.
- `internal/chatlog/wechat/service.go` — auto-decrypt + WAL monitor.
- `internal/chatlog/http/route.go` — all HTTP routes (gated by `checkDBStateMiddleware`).
- `internal/chatlog/http/service.go` — HTTP service init, MCP, hook bus, watcher.
- `internal/chatlog/temporalgraph/manager.go` + `store.go` + `digest.go` — graph subsystem.
- `internal/chatlog/semantic/manager.go` + `client.go` + `store.go` — LLM subsystem.
- `internal/chatlog/dailyreport/service.go` + `types.go` — daily report generator.

**Testing:**
- `internal/chatlog/dailyreport/*_test.go` — service/renderer/detector/vision tests.
- `internal/chatlog/semantic/client_test.go` — semantic client tests.
- `internal/chatlog/temporalgraph/*_test.go` — store, manager, digest, adaptive, buckets tests.
- `internal/chatlog/conf/semantic_test.go` — semantic config tests.
- `pkg/util/time_test.go` — utility tests.

## Naming Conventions

**Files:**
- Go source files use `snake_case.go` everywhere (e.g. `cmd_serve.go`, `digest_markdown.go`, `cmd_mac_key_helper_darwin.go`).
- Test files: same base name with `_test.go` suffix (e.g. `digest_test.go`, `manager_test.go`).
- Build-tagged platform files: `<topic>_<os>.go` (e.g. `cmd_mac_key_helper_darwin.go`, `key/darwin/...`, `process/darwin/...`, `decrypt/darwin/...`).
- Domain v4 variants use `_v4.go` (e.g. `message_v4.go`, `chatroom_v4.go`, `media_v4.go`).
- One feature per file: `internal/chatlog/temporalgraph/{digest,digest_markdown,adaptive,benchmark,helpers,store,manager,buckets,types}.go`.
- HTTP layer mirrors this: `internal/chatlog/http/{route,service,mcp,middleware,daily_report,graph,semantic_qa,hermes_qq,hermes_weixin,sns_media}.go`.

**Directories:**
- Single-purpose subpackages under `internal/chatlog/` (one package per feature).
- Per-platform variants in their own subdirectory (`key/darwin`, `key/windows`, `process/darwin`, `process/windows`, `decrypt/darwin`, `decrypt/windows`).

**Types / symbols:**
- Service types named `Service` (e.g. `database.Service`, `http.Service`, `messagehook.Service`, `chatlog/wechat.Service`).
- Manager types named `Manager` (e.g. `wechat.Manager`, `semantic.Manager`, `temporalgraph.Manager`, `chatlog.Manager`).
- Config getters: `GetXxx()` on config structs (e.g. `GetDataDir`, `GetHTTPAddr`, `GetMessageHook`).
- Errors helpers: `errors.Err`, `errors.InvalidArg`.

## Where to Add New Code

**New HTTP endpoint:**
- Primary code: add a handler in `internal/chatlog/http/route.go` and (if the route is large) split into its own file like `daily_report.go` or `graph.go`.
- Wire it in `initBaseRouter` (top-level) or `initAPIRouter` (DB-gated) in `internal/chatlog/http/route.go:57-147`.
- If the endpoint needs CLI: add a row in the `httpEndpoints` table in `cmd/chatlog/cmd_http.go`.
- Tests: prefer unit tests in the same package using `httptest`; or rely on `./init.sh` for harness coverage.

**New Cobra subcommand:**
- Primary code: create `cmd/chatlog/cmd_<name>.go` and add it to `rootCmd` inside `init()`.
- For long-running services, follow `cmd_serve.go` and call `Manager.Command<Name>()`.
- For CLI tools over the HTTP API, follow `cmd_report.go` patterns.

**New config field:**
- Add to the typed struct in `internal/chatlog/conf/{server,tui,message_hook,semantic}.go`.
- Add a default in the same file's defaults map.
- Add a `GetXxx` getter on the same struct (or on the appropriate Config interface in the consumer package).

**New TUI menu/screen:**
- Add a new widget in `internal/ui/<name>/` returning a `tview.Primitive`.
- Wire it in `internal/chatlog/app.go::initMenu` and add a tab page in `app.Run`.
- For state, mutate `ctx.Context` through a new `SetXxx` method and call `ctx.UpdateConfig()`.

**New LLM/embedding provider:**
- Add provider-specific code in `internal/chatlog/semantic/`.
- Add a constant to `internal/chatlog/conf/semantic.go` (e.g. `ProviderMMX`) and a normalize entry in `NormalizeSemanticConfig`.
- Expose runtime status (privacy-safe counters only) under `/api/v1/semantic/...`.

**New graph feature:**
- Add a new file in `internal/chatlog/temporalgraph/` (one feature per file).
- If it produces a new ingest channel: add a route in `internal/chatlog/http/route.go` and a handler file in `internal/chatlog/http/graph.go` (or a new file if it grows).
- If it produces a new aggregated output: extend `digest.go` (data) and `digest_markdown.go` (format); never print the rendered body in tests.

**New daily-report field:**
- Add to `internal/chatlog/dailyreport/types.go` and to the renderer.
- If AI analysis is needed, route through `internal/chatlog/http/daily_report.go::applyDailyAIAnalysis`.
- Always write private outputs to `reports/` and verify by path/size only.

**Utilities:**
- Cross-package: `pkg/util/<topic>.go` (e.g. `pkg/util/strings.go`, `pkg/util/time.go`).
- Single-purpose codec: `pkg/util/<codec>/` (e.g. `dat2img`, `silk`, `zstd`).
- Process/config: `pkg/process/`, `pkg/config/`.

## Special Directories

**`reports/`:**
- Purpose: output directory for daily reports; created at runtime.
- Generated: Yes.
- Committed: No (symlinked to a private external directory; not in git).

**`reports.backup-*/`:**
- Purpose: local backups of reports directory.
- Generated: Yes.
- Committed: No.

**`bin/`:**
- Purpose: build outputs from `make build`.
- Generated: Yes.
- Committed: No.

**`logs/`, `outputs/`, `.cache/`:**
- Purpose: runtime/generated artifacts.
- Generated: Yes.
- Committed: No.

**`archive/`:**
- Purpose: date-stamped task archives (`archive/YYYY-MM-DD-...`).
- Generated: snapshot by hand or by `tar`/git.
- Committed: Yes (treated as scoped work artifact per AGENTS.md).

**`tasks/`:**
- Purpose: task snapshots.
- Generated: scoped work artifacts.
- Committed: Yes (scoped to active task).

**`internal/chatlog/http/static/` and `internal/chatlog/http/wasm/`:**
- Purpose: embedded assets (`//go:embed static`) used by the HTTP server.
- Generated: No (manually maintained, but vendored third-party blobs).
- Committed: Yes.

**`internal/model/wxproto/`:**
- Purpose: generated protobuf Go types.
- Generated: Yes (do not edit by hand).
- Committed: Yes (re-generation should be a deliberate operation).

**`scripts/ralph/`:**
- Purpose: PRD -> story -> developer -> validator loop (`ralph.py`, `validate.sh`, `test_branch_merge.sh`, `prd.json`, `progress.txt`).
- Generated: No (deliberate harness assets).
- Committed: Yes.

---

*Structure analysis: 2026-06-15*
