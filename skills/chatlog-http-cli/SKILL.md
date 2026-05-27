---
name: chatlog-http-cli
description: >-
  Codex-first project harness for chatlog_alpha. Use this repo-local skill whenever
  working in this repository, calling Chatlog HTTP/CLI endpoints, validating daily
  reports, semantic search, temporal graph, MiniMax/MMX integration, or deciding
  whether an agent can claim work is complete. It combines startup map, verification
  gates, session continuity, HTTP CLI usage, and anti-false-done feedback rules.
---

# Chatlog Project Harness

This skill is only for the current `chatlog_alpha` repository. Treat the repository as the only source of truth. If something is not in the repo, a new agent should treat it as unknown until verified.

Use this skill to answer and execute four questions:

1. What is this project and where are the entrypoints?
2. Which commands prove the current change is safe?
3. What state must be written so the next session can resume?
4. When is the agent not allowed to claim completion?

## Core Harness Model

Harness = instructions + tools + environment + state + feedback.

- **Instructions**: this `SKILL.md`, repo docs, and task-specific handoff notes.
- **Tools**: Go CLI, HTTP endpoints, `chatlog http call`, `curl`, tests, build commands.
- **Environment**: local macOS/Windows Chatlog runtime, WeChat data paths, config, `127.0.0.1:5030`.
- **State**: repo files, progress notes, command evidence, generated reports, and explicit unverified items.
- **Feedback**: verification output, failed commands, HTTP responses, false-green checks, and completion audit.

Feedback is usually the cheapest high-return subsystem. Make verification commands explicit before optimizing prompts or adding more rules.

## Startup Map

Before editing or claiming an answer:

1. Confirm working directory is `chatlog_alpha`.
2. Read `README.md`, this skill, and task-relevant docs such as `docs/daily-report.md`.
3. Identify the target subsystem before touching files:
   - CLI entrypoint: `main.go`, `cmd/chatlog/`
   - HTTP API: `internal/chatlog/http/`, `cmd/chatlog/cmd_http.go`
   - Daily report: `internal/chatlog/dailyreport/`, `docs/daily-report.md`
   - Semantic/LLM provider: `internal/chatlog/semantic/`, `internal/chatlog/conf/`
   - Temporal graph: `internal/chatlog/temporalgraph/`
   - Hermes push: `internal/chatlog/hermespush/`
4. Check whether the task may touch private data, API keys, image analysis, report output, or external model quota.

Common local service:

```bash
http://127.0.0.1:5030
```

Read more: `references/harness-map.md`.

## Verification Gate

Do not say "done" unless the relevant gate has run or you explicitly say "未验证".

Minimum repo-level gates:

```bash
go test ./...
make build
go run . report daily --help
```

Focused package gates:

```bash
go test ./internal/chatlog/dailyreport
go test ./internal/chatlog/semantic
go test ./internal/chatlog/temporalgraph
```

If local HTTP service is already running, system checks:

```bash
curl -s http://127.0.0.1:5030/health
curl -s http://127.0.0.1:5030/api/v1/ping
```

Do not run quota-consuming checks by default:

```bash
chatlog report daily --vision
chatlog report daily --summary
```

These may call external or local LLM/vision providers. Run them only when the task explicitly needs it and privacy/quota risk is accepted.

Read more: `references/verification-gates.md`.

## Session Continuity

Assume every new session starts with no short-term memory. At the end of work, leave enough state for a new agent to resume in about 3 minutes.

Record:

- Current objective and affected subsystem.
- Files changed and why.
- Commands run and exact result.
- Commands not run and why.
- Blockers, risks, privacy/quota boundaries.
- Next recommended step.

Prefer durable repo-local notes over chat history when the work spans sessions. Use concise handoff content; do not paste real private report contents from `reports/`.

Read more: `references/session-state.md`.

## Feedback / Anti-False-Done

Agents are prone to premature completion. Completion must be externalized through evidence.

Three-layer completion check:

1. **Syntax**: code compiles, static checks or package tests pass.
2. **Behavior**: targeted test or CLI/HTTP behavior proves the intended path.
3. **System**: local service/report/data flow works when the task claims runtime behavior.

Rules:

- Code written is not completion.
- A generated file is not completion unless its content and provenance were checked.
- A green aggregate counter is not completion without task-level evidence.
- If verification fails, report the failing command and concrete next fix. Do not reframe failure as success.
- Do not refactor before core behavior verification passes.

Use control-variable thinking to evaluate harness changes: change one subsystem at a time, compare failure records, and attribute the real bottleneck from evidence. Harness decays like code; audit it regularly and pay down harness debt.

Read more: `references/feedback-audit.md`.

## Chatlog HTTP CLI

Use CLI to call Chatlog HTTP interfaces without opening a browser.

Command entry:

```bash
chatlog http list
chatlog http call ...
```

Common flags:

- `--addr`: server address, default `127.0.0.1:5030`
- `--timeout`: request timeout in seconds
- `--show-status`: print HTTP status
- `--output`: save response body to file

`call` flags:

- `--endpoint`: endpoint alias
- `--path`: raw path
- `--method`: HTTP method
- `--query key=value`: repeatable query param
- `--path-param key=value`: replace `{key}` in path template
- `--header key=value`: repeatable header
- `--body`: raw request body
- `--body-file`: request body file

Endpoint aliases should be checked with:

```bash
chatlog http list
```

Known important aliases:

- `health` -> `GET /health`
- `ping` -> `GET /api/v1/ping`
- `sessions` -> `GET /api/v1/sessions`
- `history` -> `GET /api/v1/history`
- `search` -> `GET /api/v1/search`
- `db_query` -> `GET /api/v1/db/query`
- `image` -> `GET /image/{key}`
- `mcp` -> `POST /mcp`

Examples:

```bash
chatlog http list
chatlog http call --endpoint history --query chat=<会话ID> --query limit=100 --query format=json
chatlog http call --endpoint search --query keyword=图片 --query limit=20
chatlog http call --endpoint db_query --query group=message --query file=message_0.db --query sql='select local_id,create_time from MSG limit 5'
chatlog http call --endpoint image --path-param key=<image_key>
chatlog http call --endpoint cache_clear --method POST
```

Most `/api/v1/*` endpoints default to YAML output unless `format=json` is provided. `--path` overrides endpoint path when both are provided.

## Automatic Hook Use

The repo also documents hook usage in `skills/chatlog-http-cli/USAGE.md`.

A project-scoped Codex `Stop` hook may run:

```bash
python3 /Volumes/WorkSSD/Dev/chatlog_alpha/.codex/hooks/chatlog_harness_skill_check.py
```

The hook only runs its check when the active working directory is inside `/Volumes/WorkSSD/Dev/chatlog_alpha`, then executes:

```bash
node skills/chatlog-http-cli/scripts/check-harness-skill.mjs
```

Treat this as a harness document/smoke gate only. It does not replace business verification such as `go test ./...`, `make build`, CLI behavior checks, or HTTP system checks.

## Required Local Skill Check

After changing this skill or its references, run:

```bash
node skills/chatlog-http-cli/scripts/check-harness-skill.mjs
```

This is a smoke/document check. It does not replace Go tests, build, HTTP runtime checks, or privacy/quota review.
