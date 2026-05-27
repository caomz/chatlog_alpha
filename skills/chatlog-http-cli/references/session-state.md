# Session State and Continuity

一句话结论：把 agent 当成每次会话都会清空短期记忆的工程师来管理。

## Handoff Template

Use this shape in final reports or repo-local progress notes when work spans sessions:

```markdown
## Current Objective

- Goal:
- Subsystem:
- Current status:

## What Changed

- File:
- Why:

## Verification Evidence

- Command:
- Result:

## Not Verified

- Command or behavior:
- Reason:

## Blockers / Risks

- Privacy:
- Quota:
- Runtime:

## Next Step

- First command or file for the next session:
```

## ACID State Rules

- **Atomic**: update status and evidence together.
- **Consistent**: do not mark done when verification contradicts it.
- **Isolated**: avoid mixing unrelated task state.
- **Durable**: record key decisions in repo-local artifacts, not only chat.

## 3-Minute Restore Target

A new Codex session should restore context by reading:

1. `README.md`
2. `skills/chatlog-http-cli/SKILL.md`
3. task-relevant doc under `docs/`
4. recent final/handoff note if present
5. failed command evidence if work is blocked
