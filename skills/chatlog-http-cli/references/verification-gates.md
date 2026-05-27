# Verification Gates

一句话结论：完成判定必须外部化，不能相信 agent 的感觉。

## Gate Levels

### 1. Syntax / Build Gate

Use when code changed:

```bash
go test ./...
make build
```

Use focused tests when touching a known subsystem:

```bash
go test ./internal/chatlog/dailyreport
go test ./internal/chatlog/semantic
go test ./internal/chatlog/temporalgraph
```

### 2. Behavior Gate

Use when CLI/report behavior changed:

```bash
go run . report daily --help
```

Use endpoint inspection when HTTP behavior changed:

```bash
go run . http list
```

### 3. System Gate

Use only if local service is already running or the task explicitly involves runtime behavior:

```bash
curl -s http://127.0.0.1:5030/health
curl -s http://127.0.0.1:5030/api/v1/ping
```

## Quota and Privacy Boundaries

Do not run these by default:

```bash
chatlog report daily --vision
chatlog report daily --summary
```

They may send image/text content to model providers or consume local/remote model quota.

## Reporting Rule

When reporting completion, include:

- Commands run
- Pass/fail result
- Any skipped gate
- Why skipped gates were safe to skip

If no verification ran, say `未验证`, not `完成`.
