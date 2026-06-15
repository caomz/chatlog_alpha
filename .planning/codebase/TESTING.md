# Testing Patterns

**Analysis Date:** 2026-06-15

This document captures the testing patterns actually used in `chatlog_alpha`.
It is derived from `internal/chatlog/*_test.go` files and the
`AGENTS.md` and `skills/chatlog-http-cli/SKILL.md` instructions. Follow these
patterns when adding or updating tests; reviewers should enforce them.

## Test Framework

**Runner:** Go's standard `testing` package (no third-party runner).

- `go.mod` does not list `testify`, `gomock`, `go-cmp`, or any test
  framework. Everything is hand-rolled on top of `t *testing.T`.
- Module Go version: `1.24.0` (`go.mod:3`).
- `Makefile:29-31` defines the canonical test command:
  ```bash
  go test ./... -cover
  ```
- The quick gate is `./init.sh`; the full gate is `./init.sh --full`
  (`init.sh:7-23, 71-74`). The full gate runs `go test ./...` and
  `make build`.

**No test framework wrappers:** every test file starts with the standard
imports:
```go
import (
    "testing"
    // plus whatever the SUT needs
)
```

**Run commands:**
```bash
go test ./...                                          # All packages
go test ./internal/chatlog/dailyreport                 # Single package
go test ./internal/chatlog/semantic ./internal/chatlog/temporalgraph
go test -run TestMiniMaxRetryDelay ./internal/chatlog/semantic
go test ./internal/chatlog/temporalgraph -run TestAdaptive
go test ./... -cover                                   # With coverage
```

## Test File Organization

**Location:** tests live next to the code under test, same package
(`package temporalgraph`, `package dailyreport`, etc.), not in a separate
`_test` package or `tests/` directory. There is no `internal/chatlog/semantic/external_test.go`
using the `_test` suffix.

**Naming:** `<file>_test.go` mirrors the source file:

| Source file | Test file |
|---|---|
| `internal/chatlog/temporalgraph/store.go` | `store_test.go` |
| `internal/chatlog/temporalgraph/digest.go` | `digest_test.go` |
| `internal/chatlog/temporalgraph/digest_markdown.go` | `digest_markdown_test.go` |
| `internal/chatlog/temporalgraph/manager.go` | `manager_test.go` |
| `internal/chatlog/temporalgraph/adaptive.go` | `adaptive_test.go` |
| `internal/chatlog/temporalgraph/buckets.go` | `buckets_test.go` |
| `internal/chatlog/temporalgraph/benchmark.go` | `benchmark_test.go` |
| `internal/chatlog/semantic/client.go` | `client_test.go` |
| `internal/chatlog/conf/semantic.go` | `semantic_test.go` |
| `internal/chatlog/dailyreport/detector.go` | `detector_test.go` |
| `internal/chatlog/dailyreport/renderer.go` | `renderer_test.go` |
| `internal/chatlog/dailyreport/service.go` | `service_test.go` |
| `internal/chatlog/dailyreport/vision.go` | `vision_test.go` |
| `pkg/util/time.go` | `pkg/util/time_test.go` |

**Structure:**
```
internal/chatlog/<pkg>/
  <file>.go
  <file>_test.go
```

## Test Structure

**Suite organization:** plain top-level `Test...` functions, not a
`TestMain` or suite struct. There is no shared `setup()` / `teardown()`
helper - each test sets up its own fixtures and tears them down with
`defer` / `t.Cleanup`.

**Table-driven tests are the norm** for pure functions and classifier
logic. Pattern (from `internal/chatlog/temporalgraph/buckets_test.go:14-52`):

```go
func TestClassifyFailedErrorCoversAllHABuckets(t *testing.T) {
    cases := []struct {
        name  string
        input string
        want  string
    }{
        {name: "config_error_exact", input: "chat model is not configured", want: "config_error"},
        {name: "sensitive_input_1026_message", input: "minimax chat error: input new_sensitive (1026)", want: "sensitive_input_1026"},
        // ...
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if got := ClassifyFailedError(tc.input); got != tc.want {
                t.Fatalf("ClassifyFailedError(%q) = %q, want %q", tc.input, got, tc.want)
            }
        })
    }
}
```

**`t.TempDir()`** is used to provide an isolated filesystem for SQLite
stores. Repeated in nearly every `temporalgraph` test
(`store_test.go:9`, `digest_test.go:15`, `buckets_test.go:83`, etc.):

```go
store, err := OpenStore(t.TempDir())
if err != nil {
    t.Fatalf("OpenStore failed: %v", err)
}
defer store.Close()
```

**`t.Setenv`** for env-driven config (`internal/chatlog/semantic/client_test.go:119-122`):

```go
clearMiniMaxEnv(t)
t.Setenv("MINIMAX_API_KEYS", " sk-cp-one1111, ,sk-cp-two2222,sk-cp-one1111 ")
t.Setenv("MINIMAX_BASE_URL", "https://example.test/v1")
```

**Time freezing:** tests construct absolute timestamps with
`time.Date(2026, 5, 27, 9, 0, 0, 0, loc)` and pass them in. There is no
mock clock; `time.Now()` is acceptable for relative-window tests (see
`digest_test.go:24-67`).

**`httptest.NewServer`** for HTTP-level tests of the semantic client
(`internal/chatlog/semantic/client_test.go:231-410`):

```go
var calls atomic.Int32
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    calls.Add(1)
    switch r.Header.Get("Authorization") {
    case "Bearer sk-cp-first1111":
        http.Error(w, "too many requests", http.StatusTooManyRequests)
    case "Bearer sk-cp-second2222":
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write([]byte(`{"choices":[{"message":{"content":" ok from second "}}]}`))
    }
}))
defer server.Close()
t.Setenv("MINIMAX_BASE_URL", server.URL)
```

**Local fixtures / builders** live alongside the test. Examples:

- `modelMsg(...)` / `imageModelMsg(...)` builders in
  `internal/chatlog/dailyreport/service_test.go:112-137` for
  `*model.Message` fixtures.
- `msg(...)` / `privateMsg(...)` builders in
  `internal/chatlog/dailyreport/detector_test.go:83-112` for
  `ChatMessage` fixtures.
- `mockDB` in `internal/chatlog/dailyreport/service_test.go:13-37` -
  hand-rolled fake that satisfies the `DBSource` interface.
- `MockConfig` / `MockChatInvoker` in
  `internal/chatlog/temporalgraph/digest_test.go:205-235`.
- `mockImageResolver` / `mockVisionClient` in
  `internal/chatlog/dailyreport/vision_test.go:12-49`.

**Test helpers** at the bottom of the test file (`clearMiniMaxEnv`,
`resetMiniMaxGlobalKeyPool`, `miniMaxTestConfig` in
`internal/chatlog/semantic/client_test.go:423-452`). Marked with
`t.Helper()` for clearer failure locations.

**Cleanup hooks:** use `t.Cleanup` to restore global state. Example:
`resetMiniMaxGlobalKeyPool` saves and restores the package-level pool
around each test (`client_test.go:436-443`):
```go
func resetMiniMaxGlobalKeyPool(t *testing.T) {
    t.Helper()
    old := miniMaxGlobalKeyPool
    miniMaxGlobalKeyPool = &miniMaxAPIKeyPool{}
    t.Cleanup(func() {
        miniMaxGlobalKeyPool = old
    })
}
```

## Mocks and Fakes

**No third-party mocking library.** All mocks are hand-rolled and live
in the test file that needs them. There is no `mocks/`, `testdata/`, or
`fixtures/` directory.

**Patterns:**

- **Interface implementation:** define a struct with the same method set
  as the production interface, often with function fields for stubbing
  return values. Example: `MockChatInvoker` in
  `digest_test.go:223-235` exposes `CallCount`, `Response`, `Error` and
  implements the `ChatInvoker` interface.
- **Inline config struct:** when only a small subset of methods is
  needed, a small struct with a `GetWorkDir() / GetSemanticConfig()`
  shape is enough. Example: `testGraphConfig` in
  `internal/chatlog/temporalgraph/manager_test.go:14-26` and
  `MockConfig` in `digest_test.go:205-220`.
- **Atomic counters** for verifying call counts under concurrency:
  `var calls atomic.Int32` (`client_test.go:230`, `client_test.go:355-369`).
  In `TestMiniMaxChatAndVisionShareOneKeyConcurrency` the
  `maxActive` counter is updated with `CompareAndSwap` to assert
  concurrency invariants.
- **Per-test isolated stores** via `OpenStore(t.TempDir())` rather than
  a shared mock store. This is preferred whenever the test depends on
  the SQLite-backed production path.

**What to mock vs what to use directly:**

| Surface | Approach |
|---|---|
| SQLite store (`temporalgraph.Store`) | Real `OpenStore(t.TempDir())` |
| LLM provider / HTTP | `httptest.NewServer` against `MINIMAX_BASE_URL` |
| Database (daily report) | Hand-rolled `mockDB` implementing `DBSource` |
| Chat / Vision client | `MockChatInvoker` / `mockVisionClient` |
| Image resolver | `mockImageResolver` |
| Config provider | Inline `MockConfig` / `testGraphConfig` |

**What is NOT mocked:**

- `time.Now()` and time-related helpers (tests pass absolute
  `time.Date(...)` values when needed; relative windows just use
  `time.Now()` and avoid asserting exact values).
- Production `conf.NormalizeSemanticConfig` / `conf.ParseHookNotifyTargets`
  - tests use them directly with synthesized inputs.
- The SQL generation layer (e.g. `store.db.Exec(...)` is used directly
  in `digest_test.go:30-64` to insert fixtures; the SUT reads through
  the same store API).

## Coverage

**Target:** none enforced as a percentage. `Makefile:31` runs
`go test ./... -cover` so coverage is collected, but no threshold is
configured.

**View coverage:**
```bash
go test ./... -cover
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
go tool cover -html=coverage.out
```

**Gap awareness:** the harness does not assert coverage. Coverage is
recorded by the developer in `progress.md` evidence if relevant.

## Test Types

**Unit tests:** the default. Almost every `_test.go` is a focused unit
test of one function or method. Examples:

- `internal/chatlog/conf/semantic_test.go` - normalize rules
- `internal/chatlog/dailyreport/detector_test.go` - mention detection
  rules
- `internal/chatlog/dailyreport/renderer_test.go` - markdown / JSON
  rendering
- `internal/chatlog/temporalgraph/digest_test.go` - window aggregation
- `pkg/util/time_test.go` - time parser (extensive table-driven)
- `internal/chatlog/semantic/client_test.go` - HTTP client retry, key
  pool, error sanitization

**Integration tests:** the project does not use a separate
`integration` build tag. "Integration" is done with real SQLite stores
in `t.TempDir()` and `httptest.NewServer` against a stubbed LLM
endpoint. Examples:

- `TestApplyExtractionAndQuery` in
  `internal/chatlog/temporalgraph/store_test.go:8-65` - real
  `OpenStore`, real SQL inserts, real `Query` call.
- `TestDigestNormalWindow` in
  `internal/chatlog/temporalgraph/digest_test.go:14-101` - end-to-end
  Digest call with inserted events, facts, relations.
- `TestChatMMXRawSwitchesKeyAfter429` in
  `internal/chatlog/semantic/client_test.go:227-258` - real
  `NewClient()` against a real local HTTP server.

**End-to-end / live-loop tests:** not in the Go tree. Live HTTP/system
checks happen outside of `go test`:

```bash
curl -s http://127.0.0.1:5030/health
curl -s http://127.0.0.1:5030/api/v1/ping
```

These are documented in `skills/chatlog-http-cli/SKILL.md` and gated
by `./init.sh --runtime`. **Do not** run them in the default
`./init.sh` quick gate.

**Benchmark tests:** `internal/chatlog/temporalgraph/benchmark_test.go`
exists (file is present in the directory) for the temporal graph
package. There is no separate `*_bench_test.go` convention; benchmarks
live in the matching `_test.go` file or in `<topic>_test.go` with `Benchmark*`
functions.

## Live-Loop Boundary

The repository distinguishes offline (no network, no real chat data)
tests from online / live tests. AGENTS.md and
`skills/chatlog-http-cli/SKILL.md` make the boundary explicit:

- The default test path under `go test ./...` must be hermetic. It uses
  `httptest.NewServer` for HTTP, `t.TempDir()` for stores, and
  `t.Setenv` for env-driven config.
- Quota / privacy-sensitive checks are explicitly excluded from the
  default gate:
  - `chatlog report daily --vision` and `chatlog report daily --summary`
    are not in `init.sh` (enforced by
    `scripts/check-root-harness.mjs:59-60`).
- Live HTTP runtime checks live behind `./init.sh --runtime` and are
  only run when a service is already listening on
  `127.0.0.1:5030`.

When writing tests:

- Never hit real LLM endpoints. Stub via `httptest.NewServer` and point
  `MINIMAX_BASE_URL` (or equivalent) at it.
- Never read or write real chat-derived data. Synthesize
  `*model.Message` / `*model.Session` / `*model.ChatRoom` fixtures
  in memory.
- Never read `.env`, `~/.mmx/config.json`, or any credential file.
  Tests must construct their own environment with `t.Setenv`.

## Test Patterns by Subsystem

**HTTP / Gin (`internal/chatlog/http/`):** there are no HTTP-level
`httptest` tests in the package - handlers are tested indirectly through
the underlying subsystems (`semantic`, `temporalgraph`, `conf`). When
adding HTTP tests, follow the pattern from `httptest.NewServer` in
`internal/chatlog/semantic/client_test.go`.

**Semantic client (`internal/chatlog/semantic/`):** heavy use of
`httptest.NewServer`, atomic counters, and env var manipulation
through `t.Setenv`. Table-driven tests for retry, error buckets, and
sanitization. Always clear env at the start
(`clearMiniMaxEnv(t)` in `client_test.go:423-434`).

**Temporal graph (`internal/chatlog/temporalgraph/`):** every test
opens its own store via `OpenStore(t.TempDir())`, uses `defer store.Close()`,
and exercises the real SQLite path. Some tests bypass helpers and use
`store.db.Exec(...)` to insert fixtures (see
`digest_test.go:30-64`); this is accepted because the alternative would
require a large fixture-builder API on `Store`.

**Daily report (`internal/chatlog/dailyreport/`):** mocks the DB with
`mockDB` (a struct with maps of fixtures). Uses
`modelMsg` / `imageModelMsg` / `msg` / `privateMsg` builders. For
vision, mocks the resolver and the vision client with
`mockImageResolver` / `mockVisionClient`
(`vision_test.go:12-49`).

**Conf (`internal/chatlog/conf/`):** small focused unit tests. Pattern:
build a `SemanticConfig` literal, call `NormalizeSemanticConfig`, assert
the result.

**Time utility (`pkg/util/time.go`):** a very large table-driven test
in `pkg/util/time_test.go` that covers RFC3339, relative, quarter,
year, month, day, and `last-Nd` windows. Comment header sections group
the cases.

## Common Patterns

**Subtests with `t.Run`:** standard. Always pass `tc.name` (or a
descriptive string). The output of `go test -v` then names each case
clearly in CI logs.

**Assertion style:** `t.Fatalf` for setup errors, `t.Errorf` /
`t.Fatalf` for assertions. Prefer `t.Fatalf` when subsequent code
would dereference nil. Examples:

```go
if got := classifyMiniMaxErrorBucket(c.err); got != c.want {
    t.Errorf("classifyMiniMaxErrorBucket(%q) = %q, want %q", c.err, got, c.want)
}
```

```go
if got, want := strings.Join(cfg.APIKeys, ","), "sk-cp-one1111,sk-cp-two2222"; got != want {
    t.Fatalf("APIKeys = %q, want %q", got, want)
}
```

**Error assertion pattern:** use `errors.Is` for sentinel errors, not
`==` (see `TestMiniMaxKeyPoolLimitsOneConcurrentRequestPerKey` in
`client_test.go:187-225`):

```go
if lease3, _, err := pool.Acquire(blockedCtx, nil); !errors.Is(err, context.DeadlineExceeded) {
    if lease3 != nil {
        lease3.Release()
    }
    t.Fatalf("third Acquire error = %v, want context deadline exceeded", err)
}
```

**Privacy-aware assertions:** the snapshot tests
(`client_test.go:454-541`) explicitly assert the response JSON does
not leak `sk-` prefixes or key fragments, and that labels are stable
ordinals (`key_1`, `key_2`, ...). Mirror this when adding endpoints
that expose provider state.

**`defer store.Close()` and `defer server.Close()`** are mandatory.
Tests that leave resources open will leak handles and break
subsequent tests in the same package.

**Imports in test files:** mirror the source file's import groups, but
add `testing` plus any mocking helpers (`httptest`, `sync/atomic`,
`os`, `path/filepath`, etc.). Use `errors` from the standard library,
not the project helper, when the SUT returns a plain `error`.

## Verifying Tests Run

**Quick check:**
```bash
go test ./internal/chatlog/semantic
go test ./internal/chatlog/temporalgraph
go test ./internal/chatlog/dailyreport
```

**Full check before claiming completion:**
```bash
./init.sh --full
```

This runs `go test ./...` and `make build` (per `init.sh:71-74`).

**Coverage hint:**
```bash
go test ./... -coverprofile=coverage.out
```

There is no enforced coverage target.

## Anti-Patterns to Avoid

- Hitting a real LLM endpoint. Always go through `httptest.NewServer`
  or a hand-rolled mock.
- Reading or writing private chat-derived content. Synthesize fixtures.
- Running `chatlog report daily --vision` or `--summary` from test
  code (or from `init.sh`); the harness script enforces this.
- Mocking the SQLite store for `temporalgraph` - prefer the real
  `OpenStore(t.TempDir())` path. The integration value outweighs the
  setup cost.
- Adding `testify`, `gomock`, or other test libraries. The project
  has deliberately not adopted them; adding them changes reviewer
  expectations and increases dependency surface.
- Using `t.Parallel()` - the codebase does not use it. Tests share
  no state today; introducing it requires audit of any future
  shared mocks.

---

*Testing analysis: 2026-06-15*
