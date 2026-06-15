# Coding Conventions

**Analysis Date:** 2026-06-15

This document is derived from the source of truth in `AGENTS.md` and the
patterns actually used in the codebase. Follow these conventions when adding or
editing code; reviewers should enforce them.

## Language and Runtime

- **Go 1.24.0** is pinned in `go.mod` line 3. Build with `CGO_ENABLED=1` (the
  project depends on `mattn/go-sqlite3` and platform-specific cgo for
  `internal/wechat/`).
- Module path: `github.com/sjzar/chatlog`.
- Use `gofmt` / `goimports` defaults; no project-local `.golangci.yml`,
  `.editorconfig`, or formatter config exists. The `Makefile` invokes
  `golangci-lint run ./...` (`Makefile:23`) but provides no config, so
  effective ruleset is upstream defaults.

## Naming Patterns

**Files and packages:**
- One package per directory. Package name is the directory name in lowercase
  (e.g. `internal/chatlog/temporalgraph` -> `package temporalgraph`).
- Test files use the standard Go suffix `*_test.go` and live next to the file
  they test (e.g. `store.go` -> `store_test.go`).
- No subpackages, no test-only folders.

**Identifiers (Go-idiomatic):**
- Exported: `PascalCase` (structs, functions, methods, types). Examples seen
  in `internal/chatlog/temporalgraph/digest.go`: `DigestOptions`,
  `DigestResult`, `DigestEntity`, `DigestEventItem`.
- Unexported: `lowerCamelCase`. Examples: `bindSingleOrBatch`,
  `parseGraphTime`, `summarizeDailyMessages`.
- Receiver names: short, consistent across the file. Prefer a 1-2 letter
  receiver. In `internal/chatlog/http/` the receiver is `s *Service`; in
  `internal/chatlog/temporalgraph/` it is `m *Manager`; in
  `internal/chatlog/semantic/manager.go` methods take a value receiver or
  `m *Manager` depending on whether they mutate state.
- HTTP handler names: `handle<Resource><Verb>` on `*Service`. Examples:
  `handleSemanticConfigGet`, `handleGraphDigest`, `handleHookConfigSet`
  (`internal/chatlog/http/route.go`).
- Typed request/response structs: short package-local types named
  `<domain><Verb>Req` / `<domain><Verb>Resp` (e.g. `hookConfigReq`,
  `semanticConfigReq`, `hookHermesWeixinReq` in
  `internal/chatlog/http/route.go:170-2898`). Inline anonymous structs are
  also acceptable for very small bodies (e.g. `graph.go:52-55`).
- Constants: `PascalCase` or `SCREAMING_SNAKE_CASE` for top-level
  configuration constants. Examples: `ProviderMMX`, `DefaultMMXChat`
  (`internal/chatlog/conf/conf.go`), `defaultMiniMaxCNBaseURL`
  (`internal/chatlog/semantic/client.go`).
- Errors and bucket names: `lower_snake_case` for portable identifiers that
  may travel over JSON. Examples: `minimax_sensitive_1026`, `chat_timeout`,
  `network_timeout` (see
  `internal/chatlog/temporalgraph/buckets_test.go:14-44`).

## Code Style

**Formatting / linting:**
- `gofmt` is the only enforced formatter (no project-specific
  `.golangci.yml`).
- `Makefile` `lint` target (`Makefile:21-23`) calls `golangci-lint run ./...`
  with default rules. `make test` (`Makefile:29-31`) is the canonical test
  command and includes `-cover`.
- Use tabs for indentation (Go default).
- Imports are grouped standard / third-party / project-local with a blank
  line between groups. Order: stdlib, third-party, `github.com/sjzar/...`
  project packages. See `internal/chatlog/http/route.go:1-34` for a
  representative example.

**Path aliases:** None used. All imports are full module paths.

**Naming for built objects:** `NewService`, `NewClient`, `NewStore`,
`NewManager` (capitalized constructors returning concrete pointer types) -
e.g. `NewService` in `internal/chatlog/http/service.go:95`, `NewClient` in
`internal/chatlog/semantic/client.go`.

## File Organization

- Public types, options, and helpers grouped near the top of the file; helpers
  lowercase near the bottom. See
  `internal/chatlog/temporalgraph/digest.go:1-80` for the canonical shape:
  `DigestOptions`, `DigestEntity`, `DigestEventItem`, `DigestResult` (top),
  then `Digest` method, then internal helpers.
- Multiple files per package by topic. `internal/chatlog/temporalgraph/` uses
  `digest.go`, `digest_markdown.go`, `manager.go`, `store.go`, `adaptive.go`,
  `buckets.go`, `benchmark.go`, `helpers.go`, `types.go`.
- HTTP handlers in `internal/chatlog/http/route.go` for primary wiring
  (`initBaseRouter`, `initAPIRouter`, `initMCPRouter`, `initMediaRouter`)
  plus per-feature files: `graph.go`, `daily_report.go`,
  `semantic_qa.go`, `hermes_qq.go`, `hermes_weixin.go`, `mcp.go`,
  `sns_media.go`, `sns_wasm_assets.go`, `service.go`, `middleware.go`.
- Persisted semantic configuration lives in `internal/chatlog/conf/`
  (specifically `semantic.go`) and is loaded through
  `LoadServiceConfig` in `conf.go`.

## Import Organization

Standard order observed in `internal/chatlog/http/route.go:1-34`:

1. `context`, `embed`, `encoding/csv`, `encoding/json`, `errors`,
   `fmt`, `io`, `io/fs`, `net/http`, `os`, `path/filepath`, `regexp`,
   `sort`, `strconv`, `strings`, `time` (stdlib)
2. `github.com/gin-gonic/gin`, `github.com/rs/zerolog/log`,
   `github.com/xuri/excelize/v2`, `gopkg.in/yaml.v3` (third-party)
3. `github.com/sjzar/chatlog/internal/...`, `github.com/sjzar/chatlog/pkg/...`
   (project-local)

Path aliases: not used. All paths are fully qualified.

## Error Handling

**Project-wide error helper** lives at `internal/errors/errors.go`:

- `errors.New(cause, code, message)`, `errors.Newf(cause, code, format, args...)`,
  `errors.Wrap(err, message, code)`, `errors.GetCode(err)`, `errors.Is(err, target)`,
  `errors.Err(c, err)` (writes to `*gin.Context`).
- The `Error` struct (`internal/errors/errors.go:13-18`) carries
  `Message`, `Cause`, `Code`, `Stack`. JSON tags omit internal fields.
- `Err(c, err)` writes the error to a Gin context as JSON with the
  status code attached. Handlers in `internal/chatlog/http/` use this
  pattern: `errors.Err(c, errors.InvalidArg("body"))` for binding failures
  (`route.go:184-199`).

**Predefined constructors** in `internal/errors/http_errors.go`:

- `InvalidArg(arg string) error` - returns
  `Newf(nil, http.StatusBadRequest, "invalid argument: %s", arg)`.
- `HTTPShutDown(cause error) error` - returns
  `Newf(cause, http.StatusInternalServerError, "http server shut down")`.

**Error buckets** (for failed model / network calls):

- Classify errors into stable `lower_snake_case` bucket strings such as
  `minimax_sensitive_1026`, `minimax_rate_limited`, `minimax_timeout`,
  `chat_timeout`, `config_error`, `json_decode_error`. Buckets surface
  in HTTP responses (e.g. `last_error_bucket` in
  `internal/chatlog/http/route.go:404-422`) and test fixtures
  (`internal/chatlog/temporalgraph/buckets_test.go:14-44`).
- Sensitive bucket names (e.g. `minimax_sensitive_1026`,
  `minimax_sensitive_1027`) must **not** trigger key rotation or requeue
  (`internal/chatlog/temporalgraph/buckets_test.go:725-759`).
- Sanitize upstream errors before returning: redact any `sk-` key fragments
  using `sanitizeMiniMaxError` (`internal/chatlog/semantic/client_test.go:412-421`).

**Privacy / quota boundary** (do not violate):

- Do not log or echo raw chat-derived content, raw API keys (`sk-...`), or
  prompt bodies containing private content.
- Verify paths, sizes, timestamps, status codes, and counts - not message
  bodies. See `AGENTS.md` "Code Patterns" and the "Privacy/quota boundary"
  callouts in `init.sh:21-23`.
- `chatlog report daily --vision` and `chatlog report daily --summary`
  are quota/privacy-sensitive; do not run them by default (enforced in
  `scripts/check-root-harness.mjs:59-60`).

## Logging

- Framework: `github.com/rs/zerolog/log` used consistently across packages:
  `internal/chatlog/manager.go:13`, `internal/chatlog/database/service.go:9`,
  `internal/chatlog/semantic/manager.go:16`, `internal/chatlog/temporalgraph/manager.go:15`,
  `internal/chatlog/http/route.go:21`, etc.
- Use structured fields, not `printf`:
  `log.Err(err).Msg("load tui config failed")` (see `conf.go:30`).
- Sanitize secrets in config logs. `internal/chatlog/conf/conf.go:97-114`
  defines `logConfig` which marshals the struct, runs
  `scrubConfigSecrets` (`conf.go:116+`), then re-marshals and logs.
- The HTTP service wires Gin's logger to `zerolog` excluding `/health`:
  `gin.LoggerWithWriter(log.Logger, "/health")` in
  `internal/chatlog/http/service.go:108`.
- The `Hermes agent 未安装` strings and Chinese prompts in user-facing
  error responses are intentional (project default is Chinese user
  communication, see `AGENTS.md`).

## Comments

- Top-of-file package-level doc comment is standard (`internal/chatlog/temporalgraph/digest.go:1`).
- Exported types and funcs have GoDoc-style comments that begin with the
  identifier name. Examples: `DigestOptions`, `DigestResult`, `DigestEntity`,
  `DigestEventItem` in `internal/chatlog/temporalgraph/digest.go:15-65`.
- Inline Chinese comments are common and intentional: e.g.
  `// ping 不依赖数据库状态，放在中间件外层，保持可用性。` in
  `internal/chatlog/http/route.go:67`. Do not strip them when editing.
- Avoid `TODO`, `FIXME`, `HACK`, `XXX` markers; no instances were found
  in `internal/chatlog/` via grep. If you need to mark incomplete work,
  record it in `progress.md` / `session-handoff.md` instead (per
  `AGENTS.md` End of Session).
- Long-form design rationale goes in the comment block on the function or
  in the relevant `docs/` file (e.g. `docs/daily-report.md`,
  `docs/graph-digest.md`).

## Function Design

- HTTP handlers are methods on `*Service`: `func (s *Service) handleX(c *gin.Context)`.
  Handler responsibilities: read request, validate, call the subsystem
  (`s.semantic`, `s.graph`, `s.db`, `s.conf`, `s.mcpServer`), and write a
  response via `writeByFormat(c, payload, c.Query("format"))`.
- Format response: `writeByFormat(c, gin.H{...}, c.Query("format"))` - emits
  JSON when `format=json` and YAML otherwise. Defined at
  `internal/chatlog/http/route.go:3218-3234`.
- Validate input before saving config or triggering runtime side effects
  (`AGENTS.md` Code Patterns). Examples:
  `handleHookConfigSet` validates `notify_mode`, `before_count >= 0`,
  `after_count >= 0` before writing to `s.conf`
  (`internal/chatlog/http/route.go:181-242`).
- Prefer existing package boundaries. CLI command wiring belongs in
  `cmd/chatlog`; HTTP handlers/routes in `internal/chatlog/http`;
  semantic provider logic in `internal/chatlog/semantic`; persisted
  semantic config in `internal/chatlog/conf`; daily report logic in
  `internal/chatlog/dailyreport`; temporal graph in
  `internal/chatlog/temporalgraph`. Do not move this code between
  packages while fixing unrelated tasks.
- Keep changes scoped to the active subsystem (see `AGENTS.md` Scope
  Rules). Do not refactor unrelated product code while fixing a
  harness, runtime, CLI, HTTP, report, or graph task.

## Module Design

- One struct per major concern, with methods grouped by lifecycle. Example:
  `Service` in `internal/chatlog/http/service.go:25-64` carries
  configuration, router, MCP server, caches, and subsystem managers.
- Internal interfaces in `internal/chatlog/semantic/manager.go:22-34`:
  `ConfigProvider` and `DBSource`. Use small interfaces to keep packages
  decoupled.
- Configuration via `viper` (`github.com/spf13/viper` v1.20.1) and the
  project helper `pkg/config`. Loaded through `LoadTUIConfig` /
  `LoadServiceConfig` in `internal/chatlog/conf/conf.go`.
- Barrel files are not used. Package exports flow through one file per
  topic.

## Configuration Conventions

- Config structs are persisted via Viper. The persisted semantic config
  lives in `internal/chatlog/conf/semantic.go`; defaults are defined as
  constants such as `DefaultMMXChat`, `DefaultGLMChat`,
  `DefaultSemanticMaxTokens`, `DefaultSemanticTemp` (used in
  `internal/chatlog/semantic/client_test.go:445-452`).
- Use `conf.NormalizeSemanticConfig(cfg)` to coerce user-supplied config
  into a canonical form (`internal/chatlog/conf/semantic_test.go:5-16`).
- Provider constants: `ProviderMMX`, `ProviderGLM`, `ProviderDeepSeek`,
  `ProviderOllama`. When switching providers, `NormalizeSemanticConfig`
  resets an MMX-specific model back to the provider default
  (`internal/chatlog/conf/semantic_test.go:18-42`).
- Environment variables: project uses `CHATLOG` prefix and `CHATLOG_DIR`
  (`internal/chatlog/conf/conf.go:16-18`). Model provider keys use
  `MINIMAX_API_KEYS` / legacy `MINMAX_API_KEY` / numbered `MINMAX_N_API_KEY`
  (see `clearMiniMaxEnv` in `internal/chatlog/semantic/client_test.go:423-434`).

## HTTP and API Conventions

- Routes registered through `(*Service).initRouter()` -> `initBaseRouter`,
  `initMediaRouter`, `initAPIRouter`, `initMCPRouter`
  (`internal/chatlog/http/route.go:50-55`).
- Routes that depend on a ready DB go under the
  `s.router.Group("/api/v1", s.checkDBStateMiddleware())` group
  (`internal/chatlog/http/route.go:112-147`).
- Bound request bodies use `c.ShouldBindJSON(&req)` and validate via
  `errors.InvalidArg("body")` on failure
  (`internal/chatlog/http/route.go:183-186`).
- All handler responses are written via `writeByFormat(c, payload, c.Query("format"))`.
  Prefer `format=json` for machine verification
  (`AGENTS.md` Code Patterns).
- Use `gin.H{"error": "..."}` directly when the message must be a
  user-readable string in a specific language, e.g.
  `gin.H{"error": "Hermes agent 未安装，无法启用 weixin 推送"}`
  (`internal/chatlog/http/route.go:205-211`).
- Streaming responses (SSE) set `Content-Type: text/event-stream;
  charset=utf-8`, `Cache-Control: no-cache`, `Connection: keep-alive`,
  then write `event: %s\ndata: %s\n\n` and flush. See
  `handleSemanticQAStream` in `internal/chatlog/http/route.go:609-643`.

## Time and Timezone Conventions

- Use `time.Date(...)` with explicit `time.Local` or `time.UTC` (not
  the zero `Location`). Examples: `internal/chatlog/dailyreport/detector_test.go:34,64,83`.
- Window strings are normalized through helpers like
  `normalizeSemanticWindowKey` (`internal/chatlog/http/route.go:1559-1584`)
  and `parseSemanticWindow` (`route.go:2720-2749`). Prefer these helpers
  to inline parsing.
- Time-of-day strings use the layout `2006-01-02 15:04:05` consistently
  for human-readable timestamps.

## Naming for Quota- and Privacy-Sensitive Code

- MiniMax key snapshots use stable ordinal labels like `key_1`, `key_2`
  instead of the raw key bytes (enforced by
  `TestMiniMaxKeyPoolStatusReportsConfiguredCountWithoutSecrets` in
  `internal/chatlog/semantic/client_test.go:454-541`).
- Failure paths that touch model providers must bucket the error
  before requeueing (`internal/chatlog/temporalgraph/buckets_test.go:82-121`).
- Reports are written under `reports/` (gitignored) and the harness
  explicitly verifies only path/size/count metadata for them
  (`AGENTS.md` Code Patterns, `init.sh:21-23`).

## Linting Quick-Reference

- Run `make lint` (or `golangci-lint run ./...`) before claiming completion.
- Run `make test` (or `go test ./... -cover`) before claiming completion.
- Use `./init.sh` for the quick gate and `./init.sh --full` for the full
  gate (`init.sh:7-23`).
- The `scripts/check-root-harness.mjs` Node script asserts that
  `init.sh` does not run `--vision` or `--summary` by default
  (`scripts/check-root-harness.mjs:59-60`). Do not add such calls to
  the default quick gate.

---

*Convention analysis: 2026-06-15*
