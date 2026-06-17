---
name: plan-feature
description: "为 chatlog_alpha 新任务创建可执行功能计划。用于把一个功能需求转成实现前计划，不写代码，必须覆盖代码上下文、验证命令、隐私/quota/runtime 边界。"
---

# Plan Feature for chatlog_alpha

Use this skill only inside `/Volumes/WorkSSD/Dev/chatlog_alpha`.

This skill replaces the project command `.agents/commands/plan-feature.md` for Codex-style skill triggering. It is project-local and must not be installed globally.

## Mission

Turn a feature request into a complete implementation plan that another coding agent can execute with high confidence.

Do not write code in this skill. The output is a plan.

## chatlog_alpha Constraints

This repository is a Go local-tool product, not a generic Web app, SaaS backend, or content project.

Planning must respect these subsystems:

- CLI: `main.go`, `cmd/chatlog/`
- HTTP API: `internal/chatlog/http/`
- Daily report: `internal/chatlog/dailyreport/`, `docs/daily-report.md`
- Semantic/LLM provider: `internal/chatlog/semantic/`, `internal/chatlog/conf/`
- Temporal graph: `internal/chatlog/temporalgraph/`
- Hermes push: `internal/chatlog/hermespush/`
- WeChat DB/key access: `internal/wechat/`, `internal/wechatdb/`
- Harness/Ralph automation: `.agents/`, `.claude/`, `scripts/ralph/`

Plans must explicitly identify privacy, model quota, runtime service, API key, and local data boundaries.

## Workflow

### 1. Understand the Feature

Clarify:

- Core problem
- User value
- Feature type: new capability, enhancement, refactor, or bug fix
- Complexity: low, medium, or high
- Affected subsystem(s)

If the requirement is ambiguous and a safe assumption is not possible, ask a concise clarifying question before writing the plan.

### 2. Gather Codebase Context

Read:

- `AGENTS.md`
- `skills/chatlog-http-cli/SKILL.md`
- `feature_list.json`
- `progress.md`
- `session-handoff.md`
- Task-relevant source files and tests

Search for similar implementations with `rg`.

Identify:

- Existing naming and file organization
- Error handling style
- HTTP route registration pattern
- CLI command registration pattern
- Existing tests to imitate
- Runtime verification path

### 3. Research External Docs Only When Needed

Use official or primary sources when the feature depends on a changing external library, API, model provider, platform policy, or current behavior.

Do not browse for stable local code facts that can be verified in the repository.

### 4. Think Through Risks

Cover:

- Edge cases
- Race conditions or worker/runtime state
- Privacy exposure
- API key and model quota usage
- macOS permissions or WeChat DB access
- Backward compatibility
- Verification and rollback

### 5. Write the Plan

Use this structure:

```markdown
# 功能: <feature-name>

## 功能描述

## 用户故事

## 问题陈述

## 方案陈述

## 功能元数据

**功能类型**:
**预估复杂度**:
**主要受影响系统**:
**依赖项**:
**隐私 / quota / runtime 边界**:

## 上下文参考

### 实现前必须阅读的代码文件

### 需要创建或修改的文件

### 需要遵循的模式

### 验证计划

### 风险与回滚

## 实施步骤

## Definition of Done
```

## Verification Guidance

Prefer concrete, low-risk gates:

```bash
./init.sh
go test ./internal/chatlog/dailyreport ./internal/chatlog/semantic ./internal/chatlog/temporalgraph
go run . report daily --help
go run . http list
```

Use runtime checks only when relevant and the service is already running:

```bash
curl -sS 'http://127.0.0.1:5030/health'
curl -sS 'http://127.0.0.1:5030/api/v1/ping'
```

Do not include quota-sensitive commands by default:

```bash
chatlog report daily --vision
chatlog report daily --summary
```

Only include them when the user explicitly authorizes model/vision calls and the task requires them.
