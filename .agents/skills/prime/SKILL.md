---
name: prime
description: "为 chatlog_alpha 代理建立代码库理解。用于加载项目上下文、查看结构、阅读核心文档、识别关键文件、检查当前分支和 dirty 状态。"
---

# Prime: Load chatlog_alpha Context

Use this skill only inside `/Volumes/WorkSSD/Dev/chatlog_alpha`.

This skill replaces the project command `.agents/commands/prime.md` for Codex-style skill triggering. It is project-local and must not be installed globally.

## Goal

Build a practical working understanding of the repository before planning, editing, reviewing, or handing work to another agent.

## Workflow

### 1. Confirm Scope

Run:

```bash
pwd
git branch --show-current
git status --short
```

The working directory must resolve to:

```text
/Volumes/WorkSSD/Dev/chatlog_alpha
```

Do not edit files during prime.

### 2. Inspect Project Structure

List tracked files:

```bash
git ls-files
```

Show a shallow directory map. Prefer `tree` when available:

```bash
tree -L 3 -I 'node_modules|__pycache__|.git|dist|build|.cache|reports|logs|outputs'
```

If `tree` is unavailable, use:

```bash
find . -maxdepth 3 \( -path './.git' -o -path './.cache' -o -path './reports' -o -path './logs' -o -path './outputs' -o -path './node_modules' -o -path './__pycache__' -o -path './dist' -o -path './build' \) -prune -o -type d -print | sed 's#^\./##' | sort
```

### 3. Read Core Documents

Read:

- `README.md`
- `AGENTS.md`
- `CLAUDE.md`
- `skills/chatlog-http-cli/SKILL.md`
- `feature_list.json`
- `progress.md`
- `session-handoff.md`
- `scripts/ralph/prd.json`
- `docs/daily-report.md` when daily report behavior may be relevant

Treat `scripts/ralph/prd.json` as a real PRD only if it contains unresolved product stories. If it is the bootstrap placeholder, say so.

### 4. Read Key Files

Read only enough to identify current architecture:

- `main.go`
- `cmd/chatlog/root.go`
- `cmd/chatlog/cmd_http.go`
- `cmd/chatlog/cmd_report.go`
- `go.mod`
- `Makefile`
- `init.sh`
- `internal/chatlog/http/route.go`
- `internal/chatlog/conf/semantic.go`
- `internal/chatlog/temporalgraph/types.go`
- Task-relevant files under `internal/chatlog/dailyreport/`, `internal/chatlog/semantic/`, `internal/chatlog/temporalgraph/`, `internal/wechat/`, or `internal/wechatdb/`

### 5. Check Recent Activity

Run:

```bash
git log -10 --oneline
git describe --tags --always --dirty='-dev'
```

## Output Format

Return a concise Chinese report with these sections:

```markdown
## Prime 完成

### 项目概览
- 应用目的和类型
- 当前版本 / 状态

### 架构
- 整体结构
- 关键目录
- 关键入口

### 技术栈
- 语言和版本
- 主要库
- 构建和测试方式

### 当前状态
- 当前分支
- active_feature_id
- dirty 文件摘要
- 当前开发重点

### 注意事项
- 隐私 / quota / runtime 风险
- tree 是否不可用等环境差异
```

## Privacy and Quota Boundary

Do not print private chat messages, report contents, API keys, model prompts, or model outputs. Verify private/runtime state through paths, timestamps, counts, status codes, and high-level fields.
