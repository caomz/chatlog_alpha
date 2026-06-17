# chatlog_alpha Agent Rules

## Project Overview

`chatlog_alpha` is a Go local-tool product for WeChat 4.x local chat data access on macOS and Windows. It provides a CLI, TUI, embedded HTTP API, MCP-style access, local report generation, semantic search, temporal graph processing, and Hermes push integrations.

Treat this repository as an AI coding automation target for local data tooling. It is not a marketing site, generic SaaS backend, or content project.

Default communication with the user should be Chinese. Keep technical names, commands, paths, config keys, and terminal output in English.

## Startup Workflow

Before writing code:

1. Confirm the working directory resolves to `/Volumes/WorkSSD/Dev/chatlog_alpha`.
2. Read `README.md`, this file, and `skills/chatlog-http-cli/SKILL.md`.
3. Check `feature_list.json` for the active feature and `progress.md` for current state.
4. Identify the subsystem before editing:
   - CLI: `main.go`, `cmd/chatlog/`
   - HTTP API: `internal/chatlog/http/`, `cmd/chatlog/cmd_http.go`
   - Daily report: `internal/chatlog/dailyreport/`, `docs/daily-report.md`
   - Semantic/LLM provider: `internal/chatlog/semantic/`, `internal/chatlog/conf/`
   - Temporal graph: `internal/chatlog/temporalgraph/`
   - Graph digest (windowed Obsidian-friendly Markdown): `internal/chatlog/temporalgraph/digest*.go`, `internal/chatlog/http/graph.go`, `cmd/chatlog/cmd_report.go`, `docs/graph-digest.md`
   - Hermes push: `internal/chatlog/hermespush/`
   - WeChat database/key access: `internal/wechat/`, `internal/wechatdb/`
   - Harness/Ralph automation: `.agents/`, `.claude/`, `scripts/ralph/`
5. Check privacy and quota risk before running commands that inspect real chat data, print report contents, call model providers, or touch API keys.

## Technology Stack

- Language/runtime: Go `1.24.0`, with cgo required for the normal local build path.
- CLI: `spf13/cobra`, `spf13/viper`.
- TUI: `rivo/tview`, `gdamore/tcell`.
- HTTP API: `gin-gonic/gin`, embedded static assets under `internal/chatlog/http/static`.
- Storage/data access: SQLite via `mattn/go-sqlite3`, local WeChat/WCDB-compatible data access under `internal/wechatdb`.
- Semantic/LLM: local Ollama defaults plus optional DeepSeek/GLM/MMX MiniMax providers through `internal/chatlog/semantic` and `internal/chatlog/conf`.
- Temporal graph: local SQLite-backed source queue, entities, facts, events, relations, timeline and graph QA under `internal/chatlog/temporalgraph`.
- Automation checks: Node scripts for harness validation; Python scripts for Ralph automation.
- Build/test: `go test`, `make build`, `./init.sh`.

## Verification Commands

Use the smallest honest gate first:

```bash
./init.sh
```

Use the full gate before claiming a broad repo-level change is complete:

```bash
./init.sh --full
```

Use the runtime gate only when the local service is already running or runtime behavior is in scope:

```bash
./init.sh --runtime
```

Focused checks from the repo-local harness:

```bash
node skills/chatlog-http-cli/scripts/check-harness-skill.mjs
node scripts/check-root-harness.mjs
go test ./internal/chatlog/dailyreport ./internal/chatlog/semantic ./internal/chatlog/temporalgraph
go run . report daily --help
go run . report graph --help
go run . http list
```

Common build/test commands:

```bash
go test ./...
make build
chatlog http list
```

Do not run quota- or privacy-sensitive checks by default:

```bash
chatlog report daily --vision
chatlog report daily --summary
```

## Structure

- `main.go`: root executable entry; delegates to `cmd/chatlog`.
- `cmd/chatlog/`: Cobra commands for TUI default mode, HTTP CLI, daily report, key helper, and hidden service startup.
- `internal/chatlog/`: application manager, HTTP service, config, semantic, daily report, temporal graph, message hook, Hermes push, and WeChat-facing services.
- `internal/wechat/`: process detection, key scanning, decryptors, and platform-specific WeChat access.
- `internal/wechatdb/`: database datasource, repository layer, and WCDB-compatible access.
- `internal/model/`: chat/contact/session/media models and generated protobuf types.
- `pkg/`: shared utility packages and version/process helpers.
- `docs/`: task-level documentation, especially daily report (`docs/daily-report.md`) and graph digest (`docs/graph-digest.md`) behavior.
- `skills/chatlog-http-cli/`: repo-local Codex harness skill and validation scripts.
- `.agents/skills/`: project-local Codex skills for this repository only, including `prime`, `plan-feature`, `create-rules`, and `source-command-create-rules`.
- `.agents/commands/`: legacy project command shortcuts kept for compatibility; do not install them globally.
- `.claude/`, `scripts/ralph/`: Claude-compatible adapters and PRD-driven automation harness.
- `archive/`, `tasks/`: task snapshots and local automation artifacts. Treat them as scoped work artifacts unless the active task says otherwise.
- `reports/`, `reports.backup-*`, `.cache/`, `logs/`, `outputs/`: generated/private/runtime outputs. Do not commit these.

## Code Patterns

- Keep changes scoped to the active subsystem and the active feature. Do not refactor unrelated product code while fixing a harness, runtime, CLI, HTTP, report, or graph task.
- Prefer existing package boundaries. CLI command wiring belongs in `cmd/chatlog`; HTTP handlers/routes in `internal/chatlog/http`; semantic provider logic in `internal/chatlog/semantic`; persisted semantic config in `internal/chatlog/conf`.
- Use typed request/response structs for HTTP handlers where existing code does. Validate input before saving config or triggering runtime side effects.
- Use `internal/errors` helpers for API errors when the surrounding handler already follows that pattern; otherwise match the local file style.
- Preserve privacy by verifying paths, timestamps, counts, status codes, and file metadata instead of printing private chat-derived message/report contents.
- Treat `format=json` as the preferred mode for machine verification of HTTP endpoints.
- Model-provider checks can consume quota and expose prompts. Run them only when the task requires it and record the privacy/quota boundary.
- When temporal graph work fails, bucket failures first with readonly evidence; do not blindly requeue sensitive/model failures.
- Generated standalone daily HTML/Markdown artifacts and report backups are private. Verify by path, size, timestamp, and high-level counts only.
- Graph digest output (`reports/graph-digest-<start>_<end>.md`) is a read-only window aggregation written idempotently (same window overwrites the same file). The default path makes zero model calls; `summary=true` makes at most one role-neutral Chat call with graceful degradation. Verify by path/size/section-count only and never print digest body; `format=json` returns metadata only.
- Do not use scripts to batch-delete files or directories. If deletion is unavoidable, delete one file at a time and explain risk/rollback first.

## Scope Rules

- One feature at a time: work only on `active_feature_id` from `feature_list.json` unless the user explicitly changes scope.
- Stay in scope: do not refactor unrelated product code while working on harness, docs, or verification.
- Track dependencies in `feature_list.json` before starting dependent work.
- If scope changes, update `feature_list.json`, `progress.md`, and `session-handoff.md` together.
- Existing dirty files may belong to the user or another run. Do not revert them unless the user explicitly asks.

## Key Files

- `README.md`: product overview, release notes, platform notes, common HTTP endpoints.
- `AGENTS.md`: root agent rules and project conventions.
- `CLAUDE.md`: imports root rules for Claude-compatible agents.
- `feature_list.json`: active feature, scope, done criteria, evidence, dependencies.
- `progress.md`: durable current state, verification evidence, skipped checks, risks, next step.
- `session-handoff.md`: restartable handoff for the next agent/session.
- `init.sh`: quick/full/runtime verification entrypoint.
- `scripts/check-root-harness.mjs`: root harness integrity check.
- `skills/chatlog-http-cli/SKILL.md`: repo-local operational skill for CLI/HTTP/report/graph verification.
- `.agents/skills/prime/SKILL.md`: project context loading skill; reads structure and state without editing files.
- `.agents/skills/plan-feature/SKILL.md`: planning-only skill for implementation plans; do not write code while using it.
- `.agents/skills/create-rules/SKILL.md`: compatibility entry for refreshing this root rules file.
- `.agents/skills/source-command-create-rules/SKILL.md`: original create-rules skill name; keep it alongside `create-rules`.
- `scripts/ralph/prd.json`: Ralph-executable PRD story list.
- `scripts/ralph/progress.txt`: Ralph iteration log and reusable Codebase Patterns.

## Ralph / PRD Automation

Ralph assets live under:

- `.agents/` for agent commands and local skills.
- `.claude/` for Claude-compatible commands and local skills.
- `scripts/ralph/` for the autonomous PRD -> story -> developer -> validator loop.

Ralph rules:

- Every Ralph run must read this file, `skills/chatlog-http-cli/SKILL.md`, `feature_list.json`, `progress.md`, `session-handoff.md`, and `scripts/ralph/prd.json` before choosing implementation work.
- Treat `scripts/ralph/prd.json` as executable only after inspecting `userStories`; if all stories are complete or the file is a bootstrap placeholder, stop and ask for a real PRD-derived story list.
- Implement exactly one unresolved `scripts/ralph/prd.json` story per iteration.
- Acceptance criteria must be concrete and verifiable through code inspection, `go test`, `./init.sh`, CLI output, HTTP status/fields, file metadata, or runtime state.
- CLI, HTTP, and runtime stories must name the exact command, endpoint, response field, file, or state transition to verify.
- Stories that inspect private chat-derived data must verify paths, timestamps, counts, status codes, or error buckets only.
- Developer agents must not commit directly. `scripts/ralph/ralph.py` performs the automatic commit only after validator success.
- Automatic commits must exclude pre-existing dirty files, `reports/`, `reports.backup-*`, `.cache/`, `logs/`, `outputs/`, `.env*`, model outputs, chat reports, and unrelated files.
- Branch lifecycle is managed only by `scripts/ralph/ralph.py`: at startup it creates/switches to the `prd.json` `branchName` work branch (skipped for empty, non-`ralph/`, or `ralph/bootstrap-placeholder` names), and after all stories pass with zero blocked it auto-merges `--no-ff` back into the base branch the run started on. Developer and validator agents must never create branches, switch branches, or merge.
- Auto-merge skips when any story is blocked, aborts cleanly on conflict while keeping the work branch, never runs `git push`, and is gated by `RALPH_AUTO_MERGE` (default on; `0`/`false`/`no` disables). Regression-test branch lifecycle via `bash scripts/ralph/test_branch_merge.sh` (sandboxed mktemp git repo, never the real repo).
- If a reusable project pattern is discovered, record it in `scripts/ralph/progress.txt`; if it affects future non-Ralph sessions, also update root `progress.md` and `session-handoff.md`.

## Definition of Done

Work is done only when:

- The active feature or explicitly-scoped side task is recorded in `feature_list.json` with evidence.
- `progress.md` records what changed, Verification Evidence, skipped checks, risks, and next step.
- `session-handoff.md` has a restartable Next Session path.
- Required verification commands ran, or skipped commands are explicitly marked `未验证`.
- Failures include the exact command, exit status, key error line, and next fix.
- Privacy/quota-sensitive commands are either avoided or explicitly justified.

Do not claim completion from code changes alone.

## End of Session

Before ending:

1. Update `progress.md` with Current State, What changed, Verification Evidence, Not Verified, Blockers/Risks, and Next.
2. Update `session-handoff.md` so a new agent can restart cleanly.
3. Keep generated private reports under `reports/` and `reports.backup-*` out of commits.
4. Do not commit, push, reset, rebase, merge, delete, or publish unless the user explicitly asked for that operation.
