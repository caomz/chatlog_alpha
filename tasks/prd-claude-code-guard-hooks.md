# PRD: Claude Code Guard Hooks for chatlog_alpha

## Introduction

把 `chatlog_alpha/AGENTS.md` 与 `CLAUDE.md` 中目前**只靠文字约定、无强制力**的硬规则，转化为 Claude Code 在工具调用前后自动执行的 hook 脚本，对**所有在该仓库工作的 AI 代理**（Claude Code、Codex 子代理、`scripts/ralph/ralph.py` 启动的 developer/validator）提供「违规即拦截」保护。

当前痛点：`.claude/` 只有 `settings.local.json`、零 hooks；AGENTS.md 包含 8 类「绝不能违反」的规则（隐私读写、批量删除、破坏性 git、提交私有报告、配额敏感命令、gofmt、DoD 状态更新），全部靠代理自觉。一旦执行模型忘记规则，就可能造成：误写/误删私有数据、`git add .` 把 `reports/` 一起暂存、未授权消耗模型配额、改完 Go 代码不更新状态文件。

本 PRD 交付 8 个 hook（6 PreToolUse + 1 PostToolUse + 1 Stop），全部以**独立 Python 脚本**形式实现，统一接进 `.claude/settings.json`（**项目共享、纳入 git**）。每个 hook 可通过「mock JSON 喂 stdin + 检查退出码」离线单测，无需真触发 Claude Code。

## Goals

- G1: 拦截对 `reports/`、`reports.backup-*/`、`.cache/`、`logs/`、`outputs/`、`.env*` 的 Write/Edit 操作（放行 `.claude/hooks/*.py` 与 `.claude/settings.json` 自身以避免热重载自死锁）。
- G2: 拦截 `cat`/`head`/`tail`/`grep`/`jq`/`sqlite3` 等命令读取 `reports/`、`reports.backup-*/`、`.cache/`、`logs/`、`outputs/`、`.env*` 下的内容。
- G3: 拦截 `rm -rf`、`find -delete`、`find -exec rm`、`git clean -fd`、含通配符的 `rm` 等批量删除命令；放行单文件删除与安全元数据查询。
- G4: 拦截破坏性 git 操作：`git commit`、`git push`、`git reset`（含 `--hard`）、`git rebase`、`git merge`、`git rm -r`、`git branch`、`git checkout`（创建/切换）、`git restore`、`git clean -fd`，以及任何**对当前 git dirty 集中路径的 revert**（如 `git checkout -- <dirty>`、`git restore <dirty>`、`git reset -- <dirty>`）。
- G5: 拦截会暂存私有目录的 `git add`（`git add .`/`-A`/`--all`、`git add reports/...`、`git add .cache/...` 等）。
- G6: 拦截配额/隐私敏感命令：`chatlog report daily --vision`、`chatlog report daily --summary`、`/api/v1/semantic/test`、`semantic test`；放行 `--help` 与普通 `chatlog report daily`。
- G7: 在 Write/Edit/MultiEdit 完 `.go` 文件后运行 `gofmt -l`；未格式化时通过 stderr 反馈给代理补 `gofmt -w`（**仅提醒，不阻断**——PostToolUse 退出码 2 在 Claude Code 中不阻断已写入的文件）。
- G8: 在 Stop 事件中检查「git status --porcelain」：若存在已修改/新增的 `*.go` 文件但 `progress.md` 不在 dirty 列表，则 stdout 输出 `{"decision":"block","reason":"..."}` 提醒补 DoD；带 `session_id` 哈希 + 侧计数文件 `.claude/hooks/.stop_block_count.<hash>` 自终止（计数 ≥ 2 时改用 `{"continue":false,"stopReason":"..."}` 强制停止，规避 Claude Code 无内置 Stop 循环防护的硬约束）。
- G9: 全部 8 个 hook 通过 `bash .claude/hooks/test_hooks.sh` 离线单测，总断言 ≥ 35 个。
- G10: 文档化「关键风险声明」5 条（热重载无安全门、subprocess 不可见、Ralph 绕过、PostToolUse 不能 block、Stop 循环防护），并明确区分本 hooks 与产品 `internal/chatlog/messagehook` 和 `/api/v1/hook/*`。

## User Stories

### US-001: 创建 hooks 目录与共享工具模块
**描述：** 作为开发者，我需要先建立 hook 基础设施与共享工具，以便后续 hook 脚本可统一读写 stdin JSON、判断路径归属、统一拦截/放行。

**Acceptance Criteria：**
- [ ] `.claude/hooks/` 目录存在
- [ ] `.claude/hooks/_common.py` 提供 `read_event()`、`deny(reason)`、`allow()`、`is_private(rel_path)`、`rel_to_project(path)` 函数
- [ ] `is_private()` 对以下路径返回 `True`：`reports/x.md`、`reports.backup-2026-06-19/x.md`、`.cache/y/z`、`logs/x.log`、`outputs/x`、`.env`、`.env.production`、`.env.local`
- [ ] `is_private()` 对以下路径返回 `False`：`.env.example`、`internal/chatlog/x.go`、`README.md`、`scripts/check-root-harness.mjs`、`wcdb_cache/x.db`（**注意**：`.gitignore` 当前不忽略 `wcdb_cache/`，但 AGENTS.md 把所有解密缓存视为私有——若 `is_private` 不覆盖此路径，需在文档中明确说明）
- [ ] `read_event()` 解析非法 JSON 时调用 `sys.exit(0)`（**放行**，绝不阻断）
- [ ] `python3 -m py_compile .claude/hooks/_common.py` 退出码 0

### US-002: 实现 `block-private-writes` hook
**描述：** 作为代理工作流安全护栏，我需要在 Write/Edit/MultiEdit 写入私有目录时阻断，以保护微信聊天数据与生成产物不被覆盖。

**Acceptance Criteria：**
- [ ] 创建 `.claude/hooks/block-private-writes.py`
- [ ] stdin 收到 `{tool_name, tool_input.file_path}` 时判定路径是否私有
- [ ] 写入 `reports/x.md` → exit 2，stderr 含 `Blocked: write to private/generated path 'reports/x.md' is forbidden`
- [ ] 写入 `internal/chatlog/x.go` → exit 0（放行）
- [ ] 写入 `.env.example` → exit 0（放行）
- [ ] **写入 `.claude/hooks/block-private-writes.py` → exit 0**（自修改放行，防热重载自死锁）
- [ ] **写入 `.claude/settings.json` → exit 0**（自配置放行）
- [ ] `tool_input` 缺 `file_path` → exit 0（放行）
- [ ] `python3 -m py_compile .claude/hooks/block-private-writes.py` 退出码 0

### US-003: 实现 `block-private-reads` hook
**描述：** 作为代理工作流安全护栏，我需要在代理用 `cat`/`head`/`tail`/`grep`/`jq`/`sqlite3` 读私有目录时阻断，以防止聊天内容被直接打印泄漏。

**Acceptance Criteria：**
- [ ] 创建 `.claude/hooks/block-private-reads.py`
- [ ] stdin 收到 `{tool_name: Bash, tool_input.command}` 时检测「读取命令 + 私有路径」组合
- [ ] `cat reports/daily.md` → exit 2
- [ ] `head -n 5 .env` → exit 2
- [ ] `tail -f logs/x.log` → exit 2
- [ ] `grep error logs/x.log` → exit 2（保守：grep 一律拦；agent 改用 `grep -c` 可放行）
- [ ] `jq '.x' reports/y.json` → exit 2
- [ ] `ls -la reports/` → exit 0（仅元数据）
- [ ] `wc -l reports/x.md` → exit 0
- [ ] `stat .env` → exit 0
- [ ] `cat internal/chatlog/x.go` → exit 0（项目代码路径放行）
- [ ] `python3 -m py_compile .claude/hooks/block-private-reads.py` 退出码 0

### US-004: 实现 `block-batch-delete` hook
**描述：** 作为代理工作流安全护栏，我需要在代理执行批量删除命令时阻断，以强制单文件删除+显式确认流程。

**Acceptance Criteria：**
- [ ] 创建 `.claude/hooks/block-batch-delete.py`
- [ ] stdin 收到 `{tool_name: Bash, tool_input.command}` 时正则匹配批量删除模式
- [ ] `rm -rf build/` → exit 2
- [ ] `rm -fr /tmp/foo` → exit 2
- [ ] `find . -name "*.tmp" -delete` → exit 2
- [ ] `find . -name "*.log" -exec rm {} \;` → exit 2
- [ ] `xargs rm < list.txt` → exit 2
- [ ] `git clean -fd` → exit 2
- [ ] `rm logs/*.log` → exit 2（含通配符即视为批量）
- [ ] `rm -f one.txt` → exit 0（单文件）
- [ ] `rm one.txt` → exit 0（单文件）
- [ ] `ls -la` → exit 0
- [ ] `python3 -m py_compile .claude/hooks/block-batch-delete.py` 退出码 0

### US-005: 实现 `block-destructive-git` hook（含 dirty-revert 检测）
**描述：** 作为代理工作流安全护栏，我需要在代理直接执行破坏性 git 操作时阻断，并在「git checkout -- / restore / reset 对当前 dirty 集中路径」时阻断，以保护现有 dirty 文件与提交历史。

**Acceptance Criteria：**
- [ ] 创建 `.claude/hooks/block-destructive-git.py`
- [ ] stdin 收到 Bash 事件时正则匹配破坏性子命令
- [ ] `git commit -m x` → exit 2
- [ ] `git push origin main` → exit 2
- [ ] `git reset --hard HEAD~1` → exit 2
- [ ] `git rebase -i HEAD~3` → exit 2
- [ ] `git merge --no-ff feature` → exit 2
- [ ] `git merge --abort` → exit 0（恢复路径放行）
- [ ] `git checkout -b foo` → exit 2（创建/切换分支）
- [ ] `git checkout foo` → exit 2（切换分支）
- [ ] `git restore CLAUDE.md` 在 CLAUDE.md 处于 dirty 时 → exit 2（**dirty-revert 命中**）
- [ ] `git restore <non-dirty-path>` → exit 0
- [ ] `git status` → exit 0
- [ ] `git rev-parse --abbrev-ref HEAD` → exit 0（ralph.py 用，放行）
- [ ] `git log --oneline -5` → exit 0
- [ ] `git diff` → exit 0
- [ ] `python3 -m py_compile .claude/hooks/block-destructive-git.py` 退出码 0
- [ ] 测试在仓库根目录执行（hook 调 `git status --porcelain` 需在 git repo 内；非 git 目录时回退到「不检测 dirty 集」、其它规则仍生效）

### US-006: 实现 `block-report-commit` hook
**描述：** 作为代理工作流安全护栏，我需要在代理用 `git add .` 等全量暂存命令时阻断，以防止 `reports/` 等私有目录被一起暂存。

**Acceptance Criteria：**
- [ ] 创建 `.claude/hooks/block-report-commit.py`
- [ ] `git add .` → exit 2
- [ ] `git add -A` → exit 2
- [ ] `git add --all` → exit 2
- [ ] `git add reports/x.md` → exit 2（显式私有路径）
- [ ] `git add internal/chatlog/x.go` → exit 0
- [ ] `git status` → exit 0（只读放行）
- [ ] `git diff --cached` → exit 0
- [ ] `python3 -m py_compile .claude/hooks/block-report-commit.py` 退出码 0

### US-007: 实现 `guard-quota-commands` hook
**描述：** 作为代理工作流安全护栏，我需要在代理默认调用配额/隐私敏感命令时阻断，以避免无意识消耗模型配额与暴露 prompt。

**Acceptance Criteria：**
- [ ] 创建 `.claude/hooks/guard-quota-commands.py`
- [ ] `chatlog report daily --vision` → exit 2
- [ ] `chatlog report daily --summary` → exit 2
- [ ] `curl http://127.0.0.1:5030/api/v1/semantic/test?format=json` → exit 2
- [ ] `chatlog semantic test` → exit 2
- [ ] `go run . report daily --help` → exit 0（`--help` 放行）
- [ ] `chatlog report daily` → exit 0（无 vision/summary）
- [ ] `chatlog http list` → exit 0
- [ ] `python3 -m py_compile .claude/hooks/guard-quota-commands.py` 退出码 0

### US-008: 实现 `gofmt-check` hook（PostToolUse）
**描述：** 作为代理工作流安全护栏，我需要在代理写完 `.go` 文件后提醒其补 gofmt，以避免未格式化代码落地但代理不知。

**Acceptance Criteria：**
- [ ] 创建 `.claude/hooks/gofmt-check.py`
- [ ] stdin 收到 Write/Edit/MultiEdit 且 `file_path` 以 `.go` 结尾时调 `gofmt -l <file>`
- [ ] 已格式化文件 → exit 0，无输出
- [ ] 未格式化文件 → exit 2，stderr 含 `Reminder (PostToolUse cannot block): '<file>' is not gofmt-formatted. Run: gofmt -w <file>`
- [ ] 非 `.go` 文件 → exit 0（不触发 gofmt）
- [ ] `gofmt` 不在 PATH 时（`FileNotFoundError`） → exit 0（不 crash、放行）
- [ ] **测试断言验证 stderr 包含 reminder 文案**（不只是退出码）
- [ ] `python3 -m py_compile .claude/hooks/gofmt-check.py` 退出码 0

### US-009: 实现 `remind-state-update` Stop hook（含自终止计数器）
**描述：** 作为代理工作流安全护栏，我需要在代理改完 `.go` 但未更新 `progress.md` 时阻止会话停止，以强制补 DoD 状态文件。

**Acceptance Criteria：**
- [ ] 创建 `.claude/hooks/remind-state-update.py`
- [ ] stdin 收到 `hook_event_name=Stop` 时：
  - 取出 `session_id`，hash 为 12 字符前缀
  - 路径 `$CLAUDE_PROJECT_DIR/.claude/hooks/.stop_block_count.<hash>` 读写计数
- [ ] 计数 = 1 且 dirty 集含 `*.go` 但无 `progress.md` → stdout `{"decision":"block","reason":"Definition of Done: ..."}`，exit 0
- [ ] 计数 ≥ 2 → stdout `{"continue":false,"stopReason":"remind-state-update: reached max block count..."}`，exit 0（**强制停止**）
- [ ] dirty 集无 `*.go` 或含 `progress.md` → stdout `{}`，exit 0（放行）
- [ ] **自终止验证**：连续 3 次 stdin 同一 session_id，第 2 次必须含 `continue.*false`，第 3 次也必须含 `continue.*false`
- [ ] `.stop_block_count.<hash>` 文件路径在 `$CLAUDE_PROJECT_DIR/.claude/hooks/` 下（**不**写 `/tmp`，避免被清理）
- [ ] **测试开头/结尾 `rm -f .claude/hooks/.stop_block_count.*`**，不污染仓库
- [ ] `python3 -m py_compile .claude/hooks/remind-state-update.py` 退出码 0

### US-010: 在 `.claude/settings.json` 挂载所有 hooks
**描述：** 作为开发者，我需要把所有 8 个 hook 接进 `.claude/settings.json`，使其对所有在仓库工作的 Claude Code 代理生效。

**Acceptance Criteria：**
- [ ] 创建 `.claude/settings.json`
- [ ] `PreToolUse` 块含 6 个 hook：`block-private-writes`（matcher `Write|Edit|MultiEdit`）、`block-private-reads`（Bash）、`block-batch-delete`（Bash）、`block-destructive-git`（Bash）、`block-report-commit`（Bash）、`guard-quota-commands`（Bash）
- [ ] `PostToolUse` 块含 1 个 hook：`gofmt-check`（matcher `Write|Edit|MultiEdit`）
- [ ] `Stop` 块含 1 个 hook：`remind-state-update`（**无 matcher**——Stop 不支持 matcher）
- [ ] 所有命令字符串使用 `python3 "$CLAUDE_PROJECT_DIR/.claude/hooks/<name>.py"` 形式（双引号保留 `$` 扩展）
- [ ] `python3 -c "import json; json.load(open('.claude/settings.json')); print('valid')"` 退出码 0
- [ ] `git check-ignore .claude/settings.json` 退出码 1（即**不被 gitignore，可正常 commit**）
- [ ] **若 `.claude/settings.json` 已存在，绝不覆盖**——先 Read，再 merge `hooks` 键（当前已确认不存在，可直接新建）

### US-011: 离线单测脚本 `test_hooks.sh`
**描述：** 作为开发者，我需要一个汇总单测脚本，让 8 个 hook 都能离线跑断言，无需启动 Claude Code 真实会话。

**Acceptance Criteria：**
- [ ] 创建 `.claude/hooks/test_hooks.sh`
- [ ] 用 `set -u`；每条用 `actual=$?; [ "$actual" = "2" ] || { echo FAIL; fail=1; }` 形式断言退出码
- [ ] 总断言数 ≥ 35（每个 hook 至少 4-5 条正/负样例）
- [ ] 临时 `.go` 文件用 `/tmp` 并在末尾 `rm -f` 清理
- [ ] 开头/结尾 `rm -f .claude/hooks/.stop_block_count.*` 清理侧计数文件
- [ ] `block-destructive-git` 的 dirty-revert 测试在仓库根目录执行（需 `.git` 目录）
- [ ] `bash .claude/hooks/test_hooks.sh` 全部通过，末尾输出 `ALL HOOK TESTS PASSED`，exit 0
- [ ] 任一断言失败 → exit 1 并打印失败项

### US-012: 文档 `docs/claude-code-hooks.md`
**描述：** 作为维护者，我需要一份说明文档，让后续开发者理解本 hooks 与产品 `messagehook` 的区别、新增/调整 hook 的流程、以及关键风险。

**Acceptance Criteria：**
- [ ] 创建 `docs/claude-code-hooks.md`
- [ ] 文档开头**显式声明**：本文件描述 Claude Code 代理 hooks（`.claude/settings.json` 配置），**不是**产品 `internal/chatlog/messagehook`、**不是** `/api/v1/hook/*` HTTP 端点
- [ ] 列出全部 8 个 hook 的触发事件、matcher、拦截规则、deny 文案样例
- [ ] 列出「关键风险声明」5 条：(1) 热重载无安全门，代理写 `settings.json` 后立即按新 hook 跑；(2) subprocess 不可见，hook 只看顶层 Bash 命令；(3) Ralph `subprocess.run(["git",...])` 绕过 hook；(4) PostToolUse 不能 block；(5) Stop 无内置循环防护
- [ ] 「临时绕过」章节说明：注释 `settings.json` 对应行（注意整块禁用副作用），或临时改 `.claude/settings.local.json`（不入 git）
- [ ] 「新增 hook」章节说明：创建 `.claude/hooks/<name>.py` → 在 `settings.json` 加 hook 块 → `bash test_hooks.sh` → PR 评审
- [ ] 文档包含 `bash .claude/hooks/test_hooks.sh` 用法示例
- [ ] `grep -q "messagehook" docs/claude-code-hooks.md && grep -q "Ralph\|ralph.py" docs/claude-code-hooks.md && grep -q "热重载\|hot reload" docs/claude-code-hooks.md` 退出码 0

### US-013: 按 DoD 更新状态文件
**描述：** 作为维护者，我需要把本特性记录到 `feature_list.json`、`progress.md`、`session-handoff.md`，让下一个 agent 能复盘与重启。

**Acceptance Criteria：**
- [ ] 在 `feature_list.json` 添加 feature 条目：
  - `id`: `claude-code-guard-hooks-2026-06-19`
  - `name`, `title`, `description`, `scope`, `done_criteria`, `evidence`, `status: "done"`
  - `evidence` 包含 `bash .claude/hooks/test_hooks.sh` 输出（截取末尾 `ALL HOOK TESTS PASSED` 行）、`python3 -c "import json; json.load(open('.claude/settings.json'))"` 输出、每个 hook 的 `py_compile` 结果
- [ ] `progress.md` 追加「What changed」「Verification Evidence」（含上述断言结果）、「Not Verified / Skipped」（手动端到端验证若未做则标 `未验证`）、「Risks」「Next」
- [ ] `session-handoff.md` 写「Next Session」路径：列出本特性的 hook 清单、test 脚本路径、关键风险、修改建议
- [ ] `python3 -c "import json; json.load(open('feature_list.json')); print('flist ok')"` 退出码 0
- [ ] `grep -q "guard-hooks\|claude-code-hooks" progress.md` 退出码 0
- [ ] `feature_list.json` 字段名/类型与已有条目（如 `graph-knowledge-digest-2026-06-10`）一致

### US-014: 根 harness 不回归
**描述：** 作为维护者，我需要确认本特性不打破仓库既有的 80/80 harness 检查与 skill 完整性检查。

**Acceptance Criteria：**
- [ ] `git status --porcelain internal/ cmd/` 输出为空（**0 改动产品代码**）
- [ ] `node scripts/check-root-harness.mjs` 退出码 0
- [ ] `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs` 退出码 0
- [ ] `./init.sh`（不带 `--full`/`--runtime`）退出码 0（快速门，不消耗配额）

### US-015: 真实闭环验证（在 Claude Code 会话中触发拦截）
**描述：** 作为维护者，我需要在真实 Claude Code 会话里触发每种 hook，确认端到端拦截行为符合预期。

**Acceptance Criteria：**
- [ ] 在 Claude Code 会话中尝试 `Write reports/_probe.md` → 被 `block-private-writes` 阻断，stderr 在模型下一轮回复中可见
- [ ] 在 Claude Code 会话中尝试 `Bash(command="cat reports/daily.md")` → 被 `block-private-reads` 阻断
- [ ] 在 Claude Code 会话中尝试 `Bash(command="rm -rf /tmp/_test")` → 被 `block-batch-delete` 阻断
- [ ] 在 Claude Code 会话中尝试 `Bash(command="git commit -m x")` → 被 `block-destructive-git` 阻断
- [ ] 在 Claude Code 会话中尝试 `Bash(command="git checkout -- CLAUDE.md")`（CLAUDE.md 是 dirty）→ 被 `block-destructive-git` dirty-revert 规则阻断
- [ ] 在 Claude Code 会话中尝试 `Bash(command="git add .")` → 被 `block-report-commit` 阻断
- [ ] 在 Claude Code 会话中尝试 `Bash(command="chatlog report daily --vision")` → 被 `guard-quota-commands` 阻断
- [ ] 在 Claude Code 会话中编辑一个故意未格式化的 `.go` 文件 → `gofmt-check` 在下一轮反馈 reminder
- [ ] 在 Claude Code 会话中改一个 `.go` 文件但不更新 `progress.md` → `remind-state-update` 在 Stop 时阻断并要求补 DoD
- [ ] 上述任一未验证 → 在 `progress.md` 显式标 `未验证`（不静默跳过）

## Functional Requirements

- FR-1: 系统必须在 `.claude/hooks/` 下提供 7 个强制 hook 脚本 + 1 个可选 Stop hook 脚本 + 1 个共享工具模块 `_common.py`
- FR-2: 每个 hook 必须独立可执行（`python3 <hook.py>`），从 stdin 读取 Claude Code JSON 事件，按退出码表达决策
- FR-3: `.claude/settings.json` 必须按事件类型挂载 hooks：PreToolUse 6 个、PostToolUse 1 个、Stop 1 个
- FR-4: `block-private-writes` 必须放行 `.claude/hooks/*.py` 与 `.claude/settings.json` 自路径
- FR-5: `block-destructive-git` 必须先调 `git status --porcelain` 取 dirty 集，再判断命令中路径是否在 dirty 集中
- FR-6: `remind-state-update` 必须用 `session_id` SHA-256 哈希的前 12 字符作为侧计数文件名的一部分，避免跨会话串扰
- FR-7: `remind-state-update` 在计数 ≥ 2 时输出 `{"continue":false,"stopReason":...}` 强制停止
- FR-8: 所有 hook 在解析 stdin 失败时**必须** `sys.exit(0)`（放行），绝不阻断
- FR-9: 所有 deny 文案只能包含**路径/命令模式名/规则编号**，绝不含聊天内容、key、prompt 等私有数据
- FR-10: `bash .claude/hooks/test_hooks.sh` 必须在仓库根目录执行并通过全部断言
- FR-11: `docs/claude-code-hooks.md` 必须明确区分本 hooks 与产品 `messagehook` 与 `/api/v1/hook/*`
- FR-12: `docs/claude-code-hooks.md` 必须列出全部 5 条关键风险声明
- FR-13: `feature_list.json` / `progress.md` / `session-handoff.md` 必须按 DoD 更新
- FR-14: `internal/` 与 `cmd/` 下**零**产品代码改动（`git status --porcelain internal/ cmd/` 输出空）

## Non-Goals

- **不覆盖 Ralph 的 git 纪律**：`scripts/ralph/ralph.py:179-186` 通过 `subprocess.run(["git",...])` 直接调 git，绕过当前会话的 Bash tool。Ralph 的 git 允许名单逻辑由 ralph.py 自身（`ralph.py:342,350,370,606`）保证，本 PRD 不重复覆盖。
- **不覆盖 subprocess 内部命令**：hook 只看顶层 Bash 命令字符串；代理写一个脚本 `Bash(command="./script.sh")`，`script.sh` 内部的 `rm -rf` 不可见。本仓库 `init.sh` 已验证干净（不含 `rm`/`git`）。
- **不强制 gofmt 落地**：`gofmt-check` 是 PostToolUse，退出码 2 在 Claude Code 中**不 block 已写入的文件**，只能事后提醒。若需硬保证 gofmt，应用 pre-commit git hook 或 CI（不在本 PRD 范围）。
- **不做 UI 验证**：本 PRD 不涉及 UI 改动；agent-browser 验证不适用。
- **不做凭据/认证 story**：本 PRD 不涉及认证、注册、登录、支付、上传、多步表单等需要闭环验收的功能——所有验收标准都通过「mock JSON + 退出码」或「git status 输出」机械验证。
- **不改 `internal/` `cmd/` 产品代码**：本 PRD 纯增量（仅 `.claude/` 与 `docs/` 与 `feature_list.json`/`progress.md`/`session-handoff.md`），不动 Go 代码、不动 tmux/CLI/HTTP 路径。
- **不做 `UserPromptSubmit` / `SessionStart` / `PreCompact` 等其他事件 hook**：本 PRD 只覆盖 PreToolUse/PostToolUse/Stop 三个事件。

## Technical Considerations

- **语言**：Python 3（macOS/Windows 都稳定；仅用标准库 `json`/`sys`/`re`/`os`/`subprocess`/`hashlib`，无第三方包）
- **配置位置**：`.claude/settings.json`（项目共享、纳入 git）；`.claude/settings.local.json` 仍保留个人本地权限（`Bash(python3 *)`、`Bash(top -l 1 -n 0)`），gitignored
- **退出码语义**：
  - PreToolUse 退出 2 → block + stderr 反馈
  - PostToolUse 退出 2 → **仅 stderr 反馈**（不 block）
  - Stop 用 stdout JSON + exit 0：`{"decision":"block","reason":...}` 或 `{"continue":false,"stopReason":...}`
  - 解析失败 / 缺字段 → 全部放行（exit 0），避免误伤
- **路径兼容**：`_common.py` 的 `rel_to_project` 与 `is_private` 必须先用 `path.replace("\\\\", "/")` 兼容 Windows 反斜杠
- **侧计数文件**：`.claude/hooks/.stop_block_count.<hash>` 必须用 `$CLAUDE_PROJECT_DIR/.claude/hooks/` 路径，不要写到 `/tmp`
- **热重载无安全门**：Claude Code 对 `settings.json` 的修改会**立即生效**，无 restart/无人工 review。`block-private-writes` 放行自路径是必要自保
- **`.claude/hooks/` 与 `.claude/settings.json` 都不在 `.gitignore`**：直接 `git add` 可入仓；只有 `.claude/settings.local.json` 在 `.gitignore:95`
- **AGENTS.md:112 隐私模式**：hook 拦截文案只含路径/模式名/规则编号，绝不打印聊天内容或 key——与项目既有隐私约定一致
- **AGENTS.md:104 私有目录清单**：`reports/`、`reports.backup-*/`、`.cache/`、`logs/`、`outputs/`、`.env*`（除 `.env.example`）
- **Ralph 旁路说明**：在 `docs/claude-code-hooks.md` 显式声明「Ralph 的 git 调用绕过 hook」

## Success Metrics

- M1: `bash .claude/hooks/test_hooks.sh` 全绿，35+ 断言全部通过
- M2: 8 个 hook 全部能离线单测（不依赖 Claude Code 真实会话）
- M3: 真实 Claude Code 端到端拦截验证 US-015 全部通过（或未验证项显式标 `未验证`）
- M4: 仓库既有 80/80 harness 检查不回归（`node scripts/check-root-harness.mjs` exit 0）
- M5: `git status --porcelain internal/ cmd/` 输出空（零产品代码改动）
- M6: `python3 -c "import json; json.load(open('.claude/settings.json'))"` exit 0（合法 JSON）
- M7: 文档含 5 条「关键风险声明」与产品 hooks 区分声明（grep 验证）

## Open Questions

- Q1: 若用户希望**仅 Claude Code** 子代理生效（不含 Codex / ralph.py 启动的 developer/validator），需要将 hooks 写到 `.claude/settings.local.json` 而非 `.claude/settings.json`？当前 PRD 默认写到 `settings.json`（团队共享），如有不同意见请在实施前告知。
- Q2: 是否需要把 `block-private-writes` 的自修改放行扩展到 `.claude/commands/` 与 `.claude/skills/`（让维护者也能自由修改命令与技能）？当前 PRD 仅放行 `.claude/hooks/` 与 `.claude/settings.json`（最小放行集）。
- Q3: 是否需要在 `block-private-reads` 中加 `python3` / `node` / `open` 等命令的拦截（防止代理通过脚本读私有文件）？当前 PRD 仅覆盖基础 cat/head/tail/grep/jq/sqlite3；若用户希望更严格拦截，请告知。
- Q4: `gofmt-check` 的「已格式化」判定是否需要兼容 `goimports` / `gofumpt` 等社区扩展工具？当前 PRD 仅用 `gofmt -l`（Go 工具链自带）。
- Q5: 当 `remind-state-update` 触发时，是否应该同时阻断 git commit / push（防止 DoD 未补就提交）？当前 PRD 仅在 Stop 时提醒，commit/push 由 `block-destructive-git` 独立拦截。
- Q6: 仓库当前 dirty 文件 `AGENTS.md` 与 `CLAUDE.md` 的 dirty-revert 测试会真实命中拦截，这**正是预期行为**；但是否需要在文档中加一段「已知 dirty 文件清单」以便维护者知道？当前 PRD 默认不列，避免误导。