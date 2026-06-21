# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

@AGENTS.md

## Architecture: How the Subsystems Connect

`Manager` (`internal/chatlog/manager.go`) is the single root struct. It owns every subsystem reference and is created once at startup by `cmd/chatlog/cmd_serve.go` (hidden `serve` command used by tmux) or the TUI root. All HTTP handlers receive the Manager and call through it — never constructing subsystem clients directly.

**Request path for most HTTP calls:**

```
cmd/chatlog/cmd_http.go
  → internal/chatlog/manager.go (Manager.Start)
    → internal/chatlog/http/server.go (gin Engine)
      → internal/chatlog/http/route.go (RegisterRoutes)
        → internal/chatlog/http/*.go (handler files per subsystem)
```

**WeChat DB access chain** (common source of bugs):

```
HTTP handler
  → internal/wechatdb/service.go (WechatDBService)
    → internal/wechatdb/datasource/wcdb/ (DataSource impl)
      → internal/wechatdb/wcdbapi/client.go (ensureDecrypted + isReadableSQLite)
        → wcdb_cache/*.db (decrypted SQLite files)
          → mattn/go-sqlite3
```

`resolveDBPath` in `client.go` maps `group=session&file=session.db` to `db_storage/session/session.db`. `isReadableSQLite` reads `sqlite_master` as the only truth test for cache validity — a file with a valid SQLite header that still fails `sqlite_master` is a bad cache entry.

**Config persistence:** Semantic and graph config is saved to disk via `internal/chatlog/conf/`. The live runtime config is separate from the service start-up config at `.cache/daily-report-config/chatlog-server.json`. A POST to `/api/v1/semantic/config` only changes the in-memory runtime; to persist across restarts, the JSON config file must also be updated.

**Temporal graph pipeline:**

```
graph source queue (internal/chatlog/temporalgraph/store.go, SQLite)
  → worker goroutines (temporalgraph/manager.go ProcessPending)
    → semantic Chat provider (internal/chatlog/semantic/client.go)
      → LLM extraction → entity/fact/event/relation tables
```

`progress_pct=100` from `/api/v1/graph/status` means `pending=0`, not `failed=0`. Always check `failed_buckets` for the real health picture.

**Frontend long-poll pattern** (`internal/chatlog/http/static/index.htm`): all fetch calls that may block long-poll responses use `fetchWithTimeout`, which registers the `AbortController` in the global `inflightControllers` Set. `switchTab` aborts and clears the entire Set. Plain `fetch()` calls for stats/graph endpoints bypass this tracker (tracked in TODOS.md as `TODO-2026-06-18-stats-long-poll-leak`).

## Open Technical Debt

Check `TODOS.md` before starting infra-level changes. Current open items:
- **TODO-2026-06-10-graph-store-wal**: add WAL + `busy_timeout` to temporal graph SQLite DSN (do after queue drains, back up DB first).
- **TODO-2026-06-18-stats-long-poll-leak**: stats and graph visualize endpoints use plain `fetch()` — tab switches leak 20+ pending requests per switch.
