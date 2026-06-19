# Claude Code 代理守卫 Hooks

> **范围声明（务必先读）**
>
> 本文件描述的是 **Claude Code 代理 hooks** —— 由 `.claude/settings.json` 配置、由 Claude Code 工具运行时（tool runtime）在 `Write`/`Edit`/`MultiEdit`/`Bash` 等工具调用前后执行的本地 Python 脚本，用来对**所有在本仓库工作的 AI 代理**（Claude Code、Codex 子代理、`scripts/ralph/ralph.py` 启动的 developer/validator）强制执行 `AGENTS.md` 中原本只靠文字约定的硬规则。
>
> 它**不是**以下任何一个，请勿混淆：
> - **不是**产品代码里的 `internal/chatlog/messagehook`（微信消息回调/推送 hook，属于运行态业务逻辑）。
> - **不是** HTTP API 的 `/api/v1/hook/*` 端点（产品对外接口）。
>
> 一句话区分：本 hooks 守护的是「AI 代理在仓库里能做什么」；`messagehook` 与 `/api/v1/hook/*` 守护的是「Chatlog 产品运行时处理什么消息」。两者代码、配置、生命周期完全独立。

## 1. 这套 hooks 解决什么问题

`AGENTS.md` / `CLAUDE.md` 列了若干「绝不能违反」的硬规则（隐私读写、批量删除、破坏性 git、提交私有报告、配额敏感命令、gofmt、DoD 状态更新），但这些规则此前**只靠代理自觉**，没有强制力。一旦执行模型忘了规则，就可能误写/误删私有数据、`git add .` 把 `reports/` 一起暂存、未授权消耗模型配额、改完 Go 代码不更新状态文件。

本套 hooks 把这些规则转成「违规即拦截」的自动护栏：

- 全部以**独立 Python 3 脚本**实现，位于 `.claude/hooks/`，仅用标准库（`json`/`sys`/`re`/`os`/`subprocess`/`hashlib`），无第三方依赖。
- 统一接进 `.claude/settings.json`（**项目共享、纳入 git**，对所有代理生效）。
- 每个 hook 都能「mock JSON 喂 stdin + 检查退出码」**离线单测**，无需启动真实 Claude Code 会话。

退出码 / 输出协议：

| 事件 | 决策方式 | 含义 |
| --- | --- | --- |
| `PreToolUse` | 退出码 `2` + stderr | **阻断**该工具调用，stderr 反馈给模型 |
| `PreToolUse` | 退出码 `0` | 放行 |
| `PostToolUse` | 退出码 `2` + stderr | **仅 stderr 提醒**（不阻断，写入已发生） |
| `Stop` | stdout JSON + 退出码 `0` | `{"decision":"block",...}` 阻止停止 / `{"continue":false,...}` 强制停止 / `{}` 放行 |
| 任意 | 解析 stdin 失败 / 缺字段 | 一律放行（退出码 `0`），绝不误伤 |

私有路径判定集中在 `_common.py` 的 `is_private()`：`reports/`、`reports.backup-*/`、`.cache/`、`logs/`、`outputs/`、`wcdb_cache/`、以及任意 `.env*`（**除** `.env.example`）。`rel_to_project()` 与 `is_private()` 都先做 Windows 反斜杠归一化（`\` → `/`），macOS / Windows 行为一致。

## 2. 全部 8 个 Hook 一览

| Hook 脚本 | 事件 | matcher | 作用 |
| --- | --- | --- | --- |
| `block-private-writes.py` | PreToolUse | `Write\|Edit\|MultiEdit` | 拦截写私有/生成路径 |
| `block-private-reads.py` | PreToolUse | `Bash` | 拦截打印私有内容的命令 |
| `block-batch-delete.py` | PreToolUse | `Bash` | 拦截批量/递归删除 |
| `block-destructive-git.py` | PreToolUse | `Bash` | 拦截破坏性 git + dirty-revert |
| `block-report-commit.py` | PreToolUse | `Bash` | 拦截会暂存私有目录的 `git add` |
| `guard-quota-commands.py` | PreToolUse | `Bash` | 拦截配额/隐私敏感命令 |
| `gofmt-check.py` | PostToolUse | `Write\|Edit\|MultiEdit` | 提醒补 gofmt（不阻断） |
| `remind-state-update.py` | Stop | （无 matcher，Stop 不支持） | 改了 `.go` 未更 `progress.md` 时阻止停止 |

所有 deny 文案**只含路径 / 命令模式名 / 规则来源**，绝不打印聊天内容、key、prompt 等私有数据（与 `AGENTS.md` 隐私约定一致）。

### 2.1 `block-private-writes`（PreToolUse / `Write|Edit|MultiEdit`）

- **拦截规则**：从 `tool_input.file_path` 取目标路径，经 `rel_to_project` 归一化后若 `is_private()` 为真 → 阻断。
- **放行例外（自保白名单）**：`.claude/hooks/**`（可改 hook 自身，防热重载自死锁）、`.claude/settings.json`（可重写挂载配置）；缺 `file_path` 也放行。
- **样例**：
  - 写 `reports/a.md` → exit 2
  - 写 `internal/chatlog/x.go` / `.env.example` / `.claude/hooks/x.py` / `.claude/settings.json` → exit 0
- **deny 文案样例**：
  ```
  Blocked: refusing to write private path 'reports/a.md' (reports/ / .cache/ / logs/ / outputs/ / .env* / wcdb_cache/ are generated artifacts — do not overwrite).
  ```

### 2.2 `block-private-reads`（PreToolUse / `Bash`）

- **拦截规则**：解析 `tool_input.command` 的各管道段命令名。
  - `grep` / `egrep` / `fgrep` 家族：**无条件拦截**（grep 打印匹配行，路径看似非私有也可能解析到私有数据，保守起见一律拦；需计数时改用 `grep -c` 走别的路径）。
  - `cat` / `head` / `tail` / `jq` / `sqlite3`：仅当命令里出现私有路径 token 时拦截。
  - `ls` / `wc` / `stat` / `file` / `du` 等仅读元数据的命令：放行（即使指向私有目录）。
- **样例**：
  - `cat reports/daily.md` / `head -n 5 .env` / `tail -f logs/x.log` / `grep error logs/x.log` / `jq .x reports/y.json` → exit 2
  - `ls -la reports/` / `wc -l reports/x.md` / `stat .env` / `cat internal/chatlog/x.go` → exit 0
- **deny 文案样例**：
  ```
  Blocked: refusing to run grep — it prints matched file content which may include private chat-derived data. Verify by path / count / metadata instead (ls, wc, stat).
  ```

### 2.3 `block-batch-delete`（PreToolUse / `Bash`）

- **拦截规则**：逐管道段匹配批量/递归删除：
  - `rm` 带递归标志（`-r`/`-R`/`-rf`/`-fr`/`--recursive`）。
  - `rm` 路径含通配符（`*` / `?` / `[`）。
  - `rm` 一次删多个显式路径。
  - `find ... -delete` 或 `find ... -exec rm`（含 `rmdir`/`unlink`）。
  - `git clean`（批量清理未跟踪文件）。
- **放行**：单文件删除（`rm one.txt` / `rm -f one.txt`）、只读命令（`ls -la`）。
- **样例**：`rm -rf build/` / `rm -fr /tmp/foo` / `find . -name "*.tmp" -delete` / `git clean -fd` / `rm logs/*.log` → exit 2；`rm -f one.txt` / `rm one.txt` / `ls -la` → exit 0。
- **deny 文案样例**：
  ```
  Blocked: refusing batch deletion (recursive rm (-r/-R)). AGENTS.md requires deleting one file at a time and explaining risk / rollback first.
  ```

### 2.4 `block-destructive-git`（PreToolUse / `Bash`，含 dirty-revert 检测）

- **始终拦截的子命令**：`git commit`、`git push`、`git reset`、`git rebase`、`git merge`（`--abort`/`--continue`/`--quit` 恢复形式放行）、`git checkout -b`/`-B`（创建/切换分支）。
- **dirty-revert 保护**：先调 `git status --porcelain` 取当前 dirty 集；对 `checkout`/`restore`/`reset` 的路径参数，若命中 dirty 集 → 阻断（防止 clobber 用户或其他 run 留下的修改）。
- **放行**：`git status`、`git diff`、`git log`、`git rev-parse --abbrev-ref HEAD`（**ralph.py 依赖，必须放行**）。
- **安全回退**：不在 git 仓库 / `git status` 失败 → dirty 集视为空，命令名规则仍生效，永不 crash。
- **样例**：`git commit -m x` / `git push origin main` / `git reset --hard HEAD~1` / `git rebase -i HEAD~3` / `git merge --no-ff feature` / `git checkout -b foo` → exit 2；在 `CLAUDE.md` 处于 dirty 时 `git restore CLAUDE.md` → exit 2；`git merge --abort` / `git status` / `git rev-parse --abbrev-ref HEAD` / `git log --oneline -5` / `git diff` → exit 0。
- **deny 文案样例**：
  ```
  Blocked: refusing destructive git operation (git commit (commits are made by scripts/ralph/ralph.py after validation)). Branch and history operations are reserved for the user / scripts/ralph/ralph.py.
  ```

### 2.5 `block-report-commit`（PreToolUse / `Bash`）

- **拦截规则**：
  - 全量暂存 `git add .` / `-A` / `--all` / `-u` / `--update` / `git add *` → 阻断（私有目录会被一起暂存）。
  - `git add <私有路径>`（如 `git add reports/x.md`）→ 经 `is_private()` 阻断。
- **放行**：`git add internal/chatlog/x.go`（显式项目代码路径）、`git status`、`git diff --cached`。
- **样例**：`git add .` / `git add -A` / `git add --all` / `git add reports/x.md` → exit 2；`git add internal/chatlog/x.go` / `git status` / `git diff --cached` → exit 0。
- **deny 文案样例**：
  ```
  Blocked: refusing to stage private/generated files (git add . stages everything (private dirs ride along)). Stage explicit project paths instead of bulk-adding, and never commit reports/, .cache/, logs/, outputs/, or .env*.
  ```

### 2.6 `guard-quota-commands`（PreToolUse / `Bash`）

- **拦截规则**：
  - `report daily --vision` / `--summary`（`chatlog` 或 `go run . report ...` 都拦）。
  - `chatlog semantic test`（live model probe）。
  - 命令含 `/api/v1/semantic/test`（如 curl，即使带 `--help` 也拦，端点本身消耗配额）。
- **放行**：含 `--help`/`-h`（不调模型）、普通 `chatlog report daily`（无 vision/summary）、只读命令（`chatlog http list`）。
- **样例**：`chatlog report daily --vision` / `chatlog report daily --summary` / `curl .../api/v1/semantic/test` / `chatlog semantic test` → exit 2；`go run . report daily --help` / `chatlog report daily` / `chatlog http list` → exit 0。
- **deny 文案样例**：
  ```
  Blocked: refusing quota/privacy-sensitive command (report daily --vision triggers vision model calls (consumes quota)). Run it only when the task and the user explicitly require model/vision calls; use --help or the non-summary path otherwise.
  ```

### 2.7 `gofmt-check`（PostToolUse / `Write|Edit|MultiEdit`，仅提醒不阻断）

- **拦截规则**：`file_path` 以 `.go` 结尾时调 `gofmt -l <file>`；输出非空（未格式化）→ 退出码 2 + stderr 提醒。
- **关键**：这是 **PostToolUse**，退出码 2 在 Claude Code 中**不阻断已写入的文件**，只在下一轮把提醒反馈给模型（见风险 §4.4）。
- **安全回退**：非 `.go` 文件 → exit 0；`gofmt` 不在 PATH（`FileNotFoundError`）/ 超时 → exit 0（不 crash、不刷屏）。
- **样例**：未格式化 `.go` → exit 2 且 stderr 含 `Reminder` 与 `gofmt -w`；已格式化 `.go` / `README.md` → exit 0 无输出。
- **stderr 文案样例**：
  ```
  Reminder: '/path/x.go' is not gofmt-formatted. Run 'gofmt -w /path/x.go' before finishing.
  ```

### 2.8 `remind-state-update`（Stop hook，含 session_id 哈希自终止计数器）

- **拦截规则**：在 `Stop` 事件取 `git status --porcelain` dirty 集；若**含 `*.go` 但不含 `progress.md`** → 视为 DoD 未满足。
- **自终止计数器（关键，见风险 §4.5）**：以 `sha256(session_id)[:12]` 命名侧计数文件 `$CLAUDE_PROJECT_DIR/.claude/hooks/.stop_block_count.<hash>`（**不写 `/tmp`**）。
  - 第 1 次（计数=1）：stdout `{"decision":"block","reason":...}` 阻止停止，要求补 `progress.md`。
  - 第 2 次及以后（计数≥2）：stdout `{"continue":false,"stopReason":...}` **强制停止**，规避 Claude Code 无内置 Stop 循环防护的硬约束。
- **放行**：dirty 集无 `*.go`，或已含 `progress.md` → stdout `{}`。
- **安全回退**：非 Stop 事件 / 不在 git 仓库 / git 失败 → `{}`，永不 crash。
- **side 文件清理**：离线单测开头/结尾执行 `rm -f .claude/hooks/.stop_block_count.*`，不污染仓库。
- **block reason 文案样例**：
  ```json
  {"decision": "block", "reason": "You modified .go files but did not update progress.md. Per AGENTS.md Definition of Done, record What changed / Verification Evidence / Risks / Next in progress.md before stopping."}
  ```

## 3. 离线单测

8 个 hook 全部能离线跑断言，无需真实 Claude Code 会话。汇总脚本：

```bash
bash .claude/hooks/test_hooks.sh
```

- 必须在**仓库根目录**执行（`block-destructive-git` 的 dirty-revert 断言需要 `.git`）。
- 脚本汇总 US-001..US-009 的正/负样例，断言总数 ≥ 35。
- 临时 `.go` 文件用 `/tmp`，开头/结尾清理 `.stop_block_count.*` 侧计数文件。
- 全绿时末尾打印 `ALL HOOK TESTS PASSED`，退出码 0；任一断言失败退出码 1 并打印失败项。

单个 hook 也可手动喂 JSON 验证，例如：

```bash
echo '{"tool_name":"Write","tool_input":{"file_path":"reports/a.md"}}' \
  | python3 .claude/hooks/block-private-writes.py; echo "exit=$?"   # 期望 exit=2
```

## 4. 关键风险声明（5 条，务必知悉）

这套 hooks 是**纵深防御**，不是密不透风的沙箱。以下 5 个边界必须清楚：

### 4.1 热重载无安全门（hot reload）

Claude Code 对 `settings.json` 与 `.claude/hooks/*.py` 的修改**立即生效**，无 restart、无人工 review。这意味着代理改完 `settings.json` 后**下一轮就按新 hook 规则运行**。因此 `block-private-writes` 必须放行 `.claude/hooks/**` 与 `.claude/settings.json` 自路径（否则改 hook 自身时会被自己拦死，形成自死锁）。反面：恶意/失误的代理理论上能写 `settings.json` 关掉 hooks——本套护栏防的是「忘记规则」，不防「主动绕过」。

### 4.2 subprocess 内部命令不可见

hook 只能看到**顶层 Bash 命令字符串**。代理写一个脚本 `Bash(command="./script.sh")`，`script.sh` 内部的 `rm -rf` / `git commit` **不可见、不被拦截**。本仓库 `init.sh` 已确认不含 `rm`/`git`，但任何新引入的脚本都绕过这层防护。

### 4.3 Ralph 通过 subprocess 调 git 绕过 hook

`scripts/ralph/ralph.py` 通过 Python `subprocess.run(["git", ...])` **直接**调用 git（分支创建、自动提交、`--no-ff` 合并），**不经过当前会话的 Bash tool**，因此 `block-destructive-git` / `block-report-commit` **拦不到 Ralph 的 git 操作**。Ralph 的 git 纪律由 `ralph.py` 自身的允许名单逻辑保证（见 `AGENTS.md` 的 Ralph 规则），本套 hooks 不重复覆盖。

### 4.4 PostToolUse 不能 block

`gofmt-check` 是 PostToolUse hook，退出码 2 在 Claude Code 中**不会撤销已写入的文件**，只能在下一轮把 stderr 提醒反馈给模型。换言之，未格式化的 `.go` 文件**已经落地**，hook 只能事后提醒补 `gofmt -w`。若需硬保证 gofmt，应使用 pre-commit git hook 或 CI（不在本套范围）。

### 4.5 Stop 无内置循环防护

Claude Code 的 Stop hook 若反复返回 `{"decision":"block"}`，会**无限阻止会话停止**（无内置循环防护）。`remind-state-update` 用 `session_id` 哈希 + 侧计数文件实现自终止：第 2 次及以后改用 `{"continue":false,"stopReason":...}` 强制停止。任何新写的 Stop hook **必须**自带类似的自终止机制，否则可能把会话卡死。

## 5. 临时绕过

当你**确有正当理由**需要执行被拦的操作时（例如确实要清理某个生成目录），有两种绕过方式：

1. **临时改 `.claude/settings.local.json`（推荐）**：该文件在 `.gitignore` 中（不入 git，个人本地生效）。在其中放一个 `hooks` 键覆盖/置空对应事件即可，不影响团队共享的 `settings.json`。用完删除或还原。
2. **注释 `settings.json` 对应行**：直接编辑团队共享的 `settings.json` 移除某个 hook 块。
   - ⚠️ 副作用：JSON **不支持注释**，只能整块删/改；且这是团队共享文件，改动会影响所有人并进入 git diff。务必用完即还原，不要提交。
   - ⚠️ 整块禁用某事件（如删掉整个 `PreToolUse`）会一次性关掉该事件下的**所有** hook，不只目标那一个。

绕过后请在 `progress.md` 记录「为什么绕过、绕过了哪个 hook、是否已还原」。

## 6. 新增 / 调整 Hook 流程

1. 在 `.claude/hooks/` 新建 `<name>.py`，复用 `_common.py` 的 `read_event()` / `deny()` / `allow()` / `is_private()` / `rel_to_project()`，保持「解析失败一律放行」的安全语义。
2. 在 `.claude/settings.json` 对应事件块（`PreToolUse` / `PostToolUse` / `Stop`）加入 hook，command 用 `python3 "$CLAUDE_PROJECT_DIR/.claude/hooks/<name>.py"` 形式（双引号保留 `$` 扩展）。`Stop` 块**不能**带 `matcher`。
3. 在 `.claude/hooks/test_hooks.sh` 补正/负样例断言（退出码或 stdout JSON），保持总断言 ≥ 35。
4. 跑 `bash .claude/hooks/test_hooks.sh` 全绿，再 `python3 -m py_compile .claude/hooks/<name>.py`。
5. 若新增 Stop hook，**必须**实现自终止机制（见 §4.5）。
6. 走 PR 评审；记得本套文件（`.claude/hooks/**`、`.claude/settings.json`）都不在 `.gitignore`，可正常 `git add` 入仓（只有 `.claude/settings.local.json` 被忽略）。

## 7. 相关文件

- `.claude/hooks/_common.py` — 共享工具（路径判定、stdin 解析、deny/allow）。
- `.claude/hooks/*.py` — 8 个 hook 脚本。
- `.claude/hooks/test_hooks.sh` — 离线单测汇总脚本。
- `.claude/settings.json` — hook 挂载配置（团队共享，纳入 git）。
- `.claude/settings.local.json` — 个人本地权限/覆盖（gitignored）。
- `AGENTS.md` / `CLAUDE.md` — 本 hooks 强制执行的原始文字规则来源。
- `scripts/ralph/ralph.py` — Ralph 自动化（其 git 调用绕过本 hooks，见 §4.3）。
