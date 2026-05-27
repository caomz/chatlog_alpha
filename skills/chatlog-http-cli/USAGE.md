# chatlog_alpha Harness Skill Usage

## Manual use in Codex

When a task touches `chatlog_alpha`, tell the agent:

```text
Use the repo-local harness skill at skills/chatlog-http-cli/SKILL.md.
Start with Startup Map, then apply Verification Gate and Feedback / Anti-False-Done before claiming completion.
```

Use the skill for four recurring modes:

- `Startup Map`: recover the project map, entrypoints, privacy boundaries, and local service port `127.0.0.1:5030`.
- `Verification Gate`: choose the smallest honest validation set, then label unrun checks as `未验证`.
- `Session Continuity`: before stopping, write what changed, why, next step, failed evidence, and unverified items.
- `Feedback / Anti-False-Done`: do not claim `完成` before syntax, behavior, or system checks actually ran.

## Manual checks

```bash
node skills/chatlog-http-cli/scripts/check-harness-skill.mjs
go test ./...
make build
go run . report daily --help
```

If the local service is already running:

```bash
curl -s http://127.0.0.1:5030/health
curl -s http://127.0.0.1:5030/api/v1/ping
```

## Automatic Codex hook

This project installs a global Codex `Stop` hook that is project-scoped by script logic:

```text
/Volumes/WorkSSD/Dev/chatlog_alpha/.codex/hooks/chatlog_harness_skill_check.py
```

On Codex turn stop, the hook runs only when the active working directory is inside `/Volumes/WorkSSD/Dev/chatlog_alpha`:

```bash
node skills/chatlog-http-cli/scripts/check-harness-skill.mjs
```

The hook is a document/smoke gate. It proves the harness skill still covers the required harness subsystems and user material. It does not replace real business validation such as `go test ./...`, `make build`, CLI behavior checks, or HTTP system checks.

If the hook fails, fix the skill or its references before writing `完成`. If business validation was not run, write `未验证` explicitly.
