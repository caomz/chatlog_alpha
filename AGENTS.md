# chatlog_alpha Agent Harness

## Startup Workflow

Before writing code:

1. Confirm the working directory is `/Volumes/WorkSSD/Dev/chatlog_alpha`.
2. Read `README.md`, this file, and `skills/chatlog-http-cli/SKILL.md`.
3. Check `feature_list.json` for the active feature and `progress.md` for current state.
4. Identify the subsystem before editing:
   - CLI: `main.go`, `cmd/chatlog/`
   - HTTP API: `internal/chatlog/http/`
   - Daily report: `internal/chatlog/dailyreport/`, `docs/daily-report.md`
   - Semantic/LLM provider: `internal/chatlog/semantic/`, `internal/chatlog/conf/`
   - Temporal graph: `internal/chatlog/temporalgraph/`
   - Hermes push: `internal/chatlog/hermespush/`
5. Check privacy and quota risk before running commands that inspect real chat data or call model providers.

## Scope Rules

- One feature at a time: work only on `active_feature_id` from `feature_list.json` unless the user explicitly changes scope.
- Stay in scope: do not refactor unrelated product code while working on harness, docs, or verification.
- Track dependencies in `feature_list.json` before starting dependent work.
- If scope changes, update `feature_list.json`, `progress.md`, and `session-handoff.md` together.

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
go run . http list
```

Do not run quota- or privacy-sensitive checks by default:

```bash
chatlog report daily --vision
chatlog report daily --summary
```

## Definition of Done

Work is done only when:

- The active feature status and evidence are updated in `feature_list.json`.
- `progress.md` records what changed, Verification Evidence, skipped checks, and why.
- `session-handoff.md` has a restartable Next Session path.
- Required verification commands ran, or skipped commands are explicitly marked `未验证`.
- Failures include the exact command, exit status, key error line, and next fix.

## End of Session

Before ending:

1. Update `progress.md` with Current State, What changed, Verification Evidence, Not Verified, Blockers, and Next.
2. Update `session-handoff.md` so a new agent can restart clean.
3. Keep generated private reports under `reports/` out of commits.
4. Do not claim completion from code changes alone.

