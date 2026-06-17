---
name: create-rules
description: "通过分析 chatlog_alpha 代码库创建或刷新项目内 AGENTS.md 全局规则。项目本地 skill，用作原 .agents/commands/create-rules.md 的兼容入口。"
---

# Create Rules for chatlog_alpha

Use this skill only inside `/Volumes/WorkSSD/Dev/chatlog_alpha`.

This skill replaces the project command `.agents/commands/create-rules.md` for Codex-style skill triggering. It is project-local and must not be installed globally.

There is also an existing compatible skill at:

```text
.agents/skills/source-command-create-rules/SKILL.md
```

Prefer keeping both names so either `create-rules` or `source-command-create-rules` can be referenced.

## Goal

Create or refresh root `AGENTS.md` so AI coding agents understand:

- What this project is
- Which technology stack it uses
- How source code is organized
- Which patterns and conventions matter
- How to build, test, verify, and safely hand off work

## Discovery

Confirm:

```bash
pwd
git status --short
```

Read:

- `README.md`
- `AGENTS.md`
- `CLAUDE.md`
- `skills/chatlog-http-cli/SKILL.md`
- `feature_list.json`
- `progress.md`
- `session-handoff.md`
- `go.mod`
- `Makefile`
- `init.sh`
- `.agents/AGENTS-template.md` if present

Note: `.cursor/AGENTS-template.md` may not exist in this repository. If missing, use `.agents/AGENTS-template.md` or the required sections below.

## Project Classification

For this repository, classify it as:

```text
Go local-tool product with CLI, TUI, embedded HTTP API, WeChat DB access, daily report, semantic search, temporal graph, Hermes push, and local agent harness automation.
```

Do not describe it as a generic Web/SaaS/content project.

## Required AGENTS.md Sections

Keep `AGENTS.md` concise and project-specific. Include:

1. Project Overview
2. Startup Workflow
3. Technology Stack
4. Verification Commands
5. Structure
6. Code Patterns
7. Scope Rules
8. Key Files
9. Ralph / PRD Automation
10. Definition of Done
11. End of Session

Preserve harness-required strings checked by `scripts/check-root-harness.mjs`, including:

- `Startup Workflow`
- `Verification Commands`
- `Definition of Done`
- `feature_list.json`
- `One feature at a time`
- `Stay in scope`
- `End of Session`
- `Ralph / PRD Automation`
- `Developer agents must not commit directly`

## Safety Rules

- Do not delete, reset, rebase, commit, push, or publish unless explicitly requested.
- Do not print private chat/report contents.
- Do not run quota-sensitive report/model commands by default.
- Do not overwrite unrelated dirty changes.
- Keep generated private reports under `reports/` out of commits.

## Verification

After creating or editing `AGENTS.md`, run:

```bash
jq empty feature_list.json
node scripts/check-root-harness.mjs
node skills/chatlog-http-cli/scripts/check-harness-skill.mjs
./init.sh
```

If this is only a rules/documentation change, `./init.sh --full` and `./init.sh --runtime` are optional and should be reported as not run unless actually needed.

## Output Format

Return:

```markdown
## 已创建或刷新全局规则

**文件**: `AGENTS.md`

### 项目类型

### 技术栈概览

### 结构

### 验证

### 后续步骤
```
