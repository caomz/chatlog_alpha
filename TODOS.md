# TODOS

## TODO-2026-06-10-graph-store-wal

- **What:** `internal/chatlog/temporalgraph/store.go:31` 的 `sql.Open("sqlite3", dbPath)` 增加 `?_journal_mode=WAL&_busy_timeout=5000` DSN 参数。
- **Why:** 当前默认 rollback journal，读写互阻；graph worker 高频写入时（5 workers），digest/status/query 等读查询可能撞 `SQLITE_BUSY`。WAL 允许读写并发，对大队列吞吐也有益。
- **Pros:** 一行改动；消除读写互阻；status 轮询与 digest 生成不再可能阻塞 worker。
- **Cons/Risk:** WAL 切换会在 db 旁生成 `-wal`/`-shm` 文件；对正在运行的生产队列做模式切换有风险。
- **Context:** 2026-06-10 /plan-eng-review 中发现（评审 graph-knowledge-digest PRD 时）。当前 store 无任何 journal/busy_timeout 配置；现有 Query/Status 端点与新 digest 端点共享此风险。
- **Depends on / blocked by:** 等 temporal graph pending 队列（当时 ~12000）消化完或维护窗口执行；执行前先备份 `temporal_graph.db`（参照 `.cache/daily-report-config/chatlog-server.json.bak-*` 的备份惯例）。
- **Status:** open
