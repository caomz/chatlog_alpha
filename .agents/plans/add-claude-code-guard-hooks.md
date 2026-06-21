# 功能: add-claude-code-guard-hooks（为 chatlog_alpha 生成 Claude Code 代理守卫 hooks）

下面这份计划应尽可能完整，但在真正开始实现前，你仍然必须再次验证文档、代码库模式以及任务本身是否合理。

特别注意：本计划生成的是 **Claude Code 代理 hooks**（配置在 `.claude/settings.json`，由 Claude Code 在工具调用前后自动执行的 shell 脚本），**不是** 本产品自身的 `internal/chatlog/messagehook` 消息钩子，也**不是** `/api/v1/hook/*` HTTP 端点。命名上必须与这两者区分开，避免混淆。

---

## 功能描述

把 `AGENTS.md` / `CLAUDE.md` 中目前**只靠文字约定、无强制力**的高风险规则，转化为 Claude Code 在工具调用前后自动执行的 **hook 脚本**，做到「违规即拦截 / 即提醒」。

当前项目状态：`.claude/` 目录下只有 `settings.local.json`（仅 2 条 permission，无任何 hooks）。AGENTS.md 里有大量强约束（隐私边界、禁批量删除、禁提交私有报告、配额敏感命令需显式授权、DoD 要求更新 `progress.md`），但这些规则**没有任何自动执行机制**——全靠代理自觉。一旦代理（尤其是 MiniMax 这种长链推理较弱的执行模型）忘记规则，就可能造成：误写/误删私有数据、把 `reports/` 提交进 git、无授权消耗模型配额、改完 Go 代码不更新状态文件。

本功能新增 5 个强制 hook + 1 个可选提醒 hook，覆盖项目最关键的 6 条规则，全部以**独立 shell 脚本**形式实现，每个脚本都能用「mock JSON 喂 stdin + 检查退出码」的方式离线单测，无需真正触发 Claude Code。

## 用户故事

作为一名 **在 chatlog_alpha 仓库上工作的 AI 代理 / 维护者**
我想要 **让 AGENTS.md 里的隐私与安全硬规则由 Claude Code hooks 自动强制执行**
以便 **即使执行模型忘记规则，也不会误写私有目录、误批量删除、误提交报告、或无授权消耗配额，从而保护真实微信聊天数据与模型配额**

## 问题陈述

`AGENTS.md` / `CLAUDE.md` 定义了多条「绝不能违反」的规则，但它们目前是**纯文档**，对代理没有强制力：

1. **隐私写入**：`reports/`、`reports.backup-*/`、`.cache/`、`logs/`、`outputs/`、`.env*` 是私有/生成产物，禁止编辑或提交（`.gitignore:46-102` 已忽略，但代理仍可能用 Write/Edit 直接写入这些路径）。
2. **隐私读取**：AGENTS.md:112 要求「verify by path/timestamp/count/status, never print private chat-derived content」。代理仍可能用 `cat reports/x.md`、`head .env`、`tail logs/y.log` 直接打印私有内容——比写入更隐蔽的隐私泄漏路径。
3. **批量删除**：AGENTS.md:119 明确「Do not use scripts to batch-delete files or directories」，但没有任何机制阻止代理执行 `rm -rf`、`find ... -delete`。
4. **破坏性 git 命令**：AGENTS.md:193 明确「Do not commit, push, reset, rebase, merge, delete, or publish unless the user explicitly asked」；AGENTS.md:127 明确「Do not revert dirty files unless asked」。`git commit`、`git push`、`git reset --hard`、`git rebase`、`git merge`、`git checkout -- <dirty>`、`git restore <dirty>`、`git clean -fd` 都应被拦截。
5. **提交私有报告**：DoD 要求「Keep generated private reports out of commits」，但 `git add .` / `git add -A` 会把私有产物一起暂存。
6. **配额/隐私敏感命令**：`chatlog report daily --vision|--summary`、`semantic/test` 会消耗模型配额并暴露 prompt，AGENTS.md:81-86 要求「Do not run by default」，但没有拦截。
7. **代码风格**：Go 代码改完应 `gofmt`，但容易忘记，导致 `./init.sh` 失败。
8. **DoD 状态更新**：改了产品代码却忘记更新 `progress.md` / `session-handoff.md`，违反 Definition of Done。

本计划以 **6 个 PreToolUse 拦截 + 1 个 PostToolUse 提醒 + 1 个可选 Stop 自检** 覆盖上述 8 条规则。

## 方案陈述

在 `.claude/hooks/` 下新增一组**独立、可单测的 hook 脚本**（统一用 `python3` 编写，因为 `settings.local.json` 已允许 `Bash(python3 *)`，且 Python 在 macOS/Windows 都稳定、JSON 解析无依赖），并在 `.claude/settings.json`（**项目共享、纳入 git**）中按事件类型挂载：

- `PreToolUse` + matcher `Write|Edit|MultiEdit` → `block-private-writes.py`：拦截对私有/生成目录的写入（放行 `.claude/hooks/` 与 `.claude/settings.json` 自身，避免 hook 自修改时死锁）。
- `PreToolUse` + matcher `Bash` → `block-private-reads.py`：拦截 `cat`/`head`/`tail`/`less`/`jq` 等对私有目录的读取命令。
- `PreToolUse` + matcher `Bash` → `block-batch-delete.py`：拦截 `rm -rf`、`find -delete` 等批量删除。
- `PreToolUse` + matcher `Bash` → `block-destructive-git.py`：拦截 `git commit`/`push`/`reset --hard`/`rebase`/`merge`/`clean -fd`/`checkout -- <dirty>`/`restore <dirty>`。
- `PreToolUse` + matcher `Bash` → `block-report-commit.py`：拦截会暂存私有目录的 `git add`。
- `PreToolUse` + matcher `Bash` → `guard-quota-commands.py`：拦截配额/隐私敏感命令（vision/summary/semantic test）。
- `PostToolUse` + matcher `Write|Edit|MultiEdit` → `gofmt-check.py`：改完 `.go` 文件后检查 `gofmt -l`，未格式化则通过 stderr 反馈提醒（注意：PostToolUse 退出码 2 不会阻断，只能反馈；这是 Claude Code 的硬约束）。
- （可选）`Stop` → `remind-state-update.py`：若有 `.go` 改动但 `progress.md` 未更新，用 JSON `decision:block` 提醒补 DoD；**自终止设计**为：脚本读 `transcript_path` 累计本会话的 block 次数，达到 2 次后改用 `continue:false` + `stopReason` 强制停止，避免无限循环（Claude Code 没有内置的 Stop 循环防护）。

**拦截机制统一用「退出码 2 + stderr 写原因」**：Claude Code 对 `PreToolUse` 退出码 2 会阻断该次工具调用并把 stderr 反馈给模型；`PostToolUse` 退出码 2 **不会阻断**（文件已写入）但会把 stderr 反馈给模型作提醒；`Stop` 事件用 `{"decision":"block","reason":...}`（stdout + 退出 0）做阻断。允许时退出 0。脚本路径用 `$CLAUDE_PROJECT_DIR` 环境变量（Claude Code 注入，指向项目根）保证可移植。

**关键风险声明**（实现前必须读到）：
1. **热重载无安全门**：Claude Code 对 `settings.json` 的编辑会**立即生效**，没有 restart 或人工 review 步骤。代理若在会话中**写** `.claude/settings.json` 或 `.claude/hooks/*.py`，新规则立即对自己的下一次工具调用生效。`block-private-writes` 必须显式**放行** `.claude/hooks/` 与 `.claude/settings.json` 路径——否则实现者（你）写完 hook 脚本后再次 Edit 它们将被自己写的 hook 阻断。
2. **subprocess 不可拦截**：hooks 只看顶层 `Bash` 命令字符串。`./init.sh` 内部若调用 `rm -rf`（本仓库无此情况，已验证），hook 看不到，只能靠外部 `if` 权限规则或修改子脚本配合。本仓库 `init.sh` 干净（已验证不含 `rm`/`git`），因此风险点仅在于代理写的临时脚本——agent 被拦截后须主动改用非破坏性子命令或拆单文件删除。
3. **Ralph 绕过 hooks**：`scripts/ralph/ralph.py` 通过 `subprocess.run(["git", ...])` 直接调用 git（`ralph.py:179-186`），**不经过**当前会话的 Bash tool，hook 看不到 ralph 内部的 commit/merge/branch。本计划只拦截当前代理**直接**在 Bash tool 调用的 git 命令；ralph 的 git 纪律由 ralph.py 自身的 allowlist 逻辑保证（`ralph.py:342,350,370,606`），本计划不重复覆盖，并在文档中明确声明。
4. **PostToolUse 不能 block**：`gofmt-check` 只能做事后提醒，无法阻止未格式化代码落地。若需强制 gofmt 落地，应改用 **pre-commit git hook**（不在本计划范围）或 CI 检查。
5. **Stop hook 无内置循环防护**：必须自实现计数器（基于 `transcript_path` 侧计数文件或内存），否则代理会卡在「阻断—补 DoD—再补 DoD」的无限循环里。

## 功能元数据

**功能类型**: 新能力（代理工作流安全护栏）
**预估复杂度**: 中（脚本逻辑简单，但事件接线、退出码语义、跨平台需要精确）
**主要受影响系统**: `.claude/`（Claude Code 配置层）。**不触碰任何 Go 产品代码**，不触碰 `internal/`、`cmd/`。
**依赖项**: `python3`（已确认 `/opt/homebrew/bin/python3` Python 3.14；仅用标准库 `json`/`sys`/`re`/`os`/`subprocess`，无第三方包）；`git`（已在仓库内）；`gofmt`（随 Go 工具链，已确认 Go 1.24）。

---

## 上下文参考

### 相关代码文件 重要：实现前你必须先阅读这些文件！

- `/Volumes/WorkSSD/Dev/chatlog_alpha/.claude/settings.local.json` — 原因：现有 permission 配置（`Bash(python3 *)`、`Bash(top -l 1 -n 0)`）。**理解 settings.local.json（个人、被 gitignore）与 settings.json（项目共享、纳入 git）的区别**：本功能的 hooks 要写进 `settings.json` 以便对所有在本仓库工作的代理生效。
- `/Volumes/WorkSSD/Dev/chatlog_alpha/AGENTS.md` — 原因：所有 hook 规则的来源。重点段落：
  - 「Code Patterns」末尾：`Do not use scripts to batch-delete files or directories. If deletion is unavoidable, delete one file at a time and explain risk/rollback first.`
  - 「Verification Commands」：`Do not run quota- or privacy-sensitive checks by default:` 后列出 `chatlog report daily --vision` / `--summary`。
  - 「Definition of Done」/「End of Session」：要求更新 `progress.md`、`session-handoff.md`，且「Keep generated private reports under `reports/` and `reports.backup-*` out of commits」。
  - 「Structure」：`reports/`, `reports.backup-*`, `.cache/`, `logs/`, `outputs/` 是 generated/private/runtime 输出，不可提交。
- `/Volumes/WorkSSD/Dev/chatlog_alpha/.gitignore` (lines 31-103) — 原因：私有/生成目录的**权威清单**，hook 的拦截路径必须与之一致：`.env`、`.env.*`（保留 `.env.example`）、`.cache/`、`.gocache/`、`.gocache_local/`、`.gomodcache/`、`logs/`、`reports/`、`outputs/`、`/reports`、`/reports.backup-*/`、`__pycache__/`。
- `/Volumes/WorkSSD/Dev/chatlog_alpha/CLAUDE.md` — 原因：架构总览；确认 hooks 不应干扰 Manager/HTTP/temporalgraph 等子系统。
- `/Volumes/WorkSSD/Dev/chatlog_alpha/scripts/chatlog-ha-guard.sh` — 原因：项目内既有 shell 守卫脚本的**风格参考**（如何在不打印 secret 的前提下做检查、如何报告 count）。新 hook 脚本应保持同样的「只报路径/计数/状态、不打印私有内容」原则。

### 需要创建的新文件

- `.claude/hooks/block-private-writes.py` — PreToolUse/Write|Edit|MultiEdit：拦截对私有/生成目录的写入；放行 `.claude/hooks/*.py` 与 `.claude/settings.json`（防自死锁）。
- `.claude/hooks/block-private-reads.py` — PreToolUse/Bash：拦截 `cat`/`head`/`tail`/`grep`/`jq`/`sqlite3` 等读取私有目录的命令。
- `.claude/hooks/block-batch-delete.py` — PreToolUse/Bash：拦截 `rm -rf`、`find -delete`、`git clean -fd`、含通配符的 `rm`。
- `.claude/hooks/block-destructive-git.py` — PreToolUse/Bash：拦截 `git commit`/`push`/`reset --hard`/`rebase`/`merge`/`branch`/`checkout`/`restore`，以及 dirty-revert。
- `.claude/hooks/block-report-commit.py` — PreToolUse/Bash：拦截会暂存私有目录的 `git add`（`git add .` / `-A` / 显式私有路径）。
- `.claude/hooks/guard-quota-commands.py` — PreToolUse/Bash：拦截配额/隐私敏感命令（`report daily --vision|--summary`、`semantic test`）。
- `.claude/hooks/gofmt-check.py` — PostToolUse/Write|Edit|MultiEdit：改完 `.go` 后 `gofmt -l` 提醒（注意：PostToolUse 不能 block）。
- `.claude/hooks/remind-state-update.py` —（可选）Stop：改 .go 但 progress.md 未改 → 补 DoD；带 session_id 哈希 + 侧计数器自终止。
- `.claude/hooks/_common.py` — 共享工具：stdin JSON 读取、`is_private`、`deny`/`allow`、Windows 路径 normalize。
- `.claude/settings.json` — **新建**项目共享配置，挂载 6 PreToolUse + 1 PostToolUse（+ 可选 1 Stop）= 共 8 个 hook。当前仓库不存在此文件，可直接新建。
- `.claude/hooks/test_hooks.sh` — 离线单测脚本：对每个 hook 喂 mock JSON 并断言退出码（35+ 断言）。
- `docs/claude-code-hooks.md` — hooks 行为、维护、临时绕过、新增流程；含 5 条「关键风险声明」。

### 相关文档 实现前你应该先阅读这些文档！

- [Claude Code Hooks 参考](https://code.claude.com/docs/en/hooks.md)（优先 URL；docs.claude.com/en 旧路径亦可）
  - 具体章节：Hook 事件类型（PreToolUse / PostToolUse / Stop）、stdin JSON schema、**退出码 2 在不同事件下的语义差异**、matcher 写法（区分「字符串精确匹配」与「JS 正则」两种模式）、`$CLAUDE_PROJECT_DIR`、热重载行为、Stop 的 `continue:false` 兜底。
  - 原因：本功能的核心契约。**关键事实（实现前必须逐条 confirm）**：
    - stdin JSON 公共字段：`session_id`、`transcript_path`、`cwd`、`hook_event_name`、`tool_name`、`tool_input`；PostToolUse 额外有 `tool_response`；Stop 没有 `tool_name`/`tool_input`，但有 `session_id` 可用。
    - **`tool_input.file_path`** 是 Write/Edit/MultiEdit 的字段；**`tool_input.command`** 是 Bash 的字段。
    - **退出码 2** 在 PreToolUse → block + stderr 反馈；**在 PostToolUse 仅 stderr 反馈（不 block，文件已落盘）**；在 Stop → 阻断。退出码 0 放行；非零非 2 → 非阻断错误。
    - **Match 没 match 上的事件**（如 Stop、UserPromptSubmit、CwdChanged）**不支持 matcher 字段**——`Stop` 块的 hooks 数组直接挂载，无 `matcher` 键。
    - **JSON stdout 在 exit 0 时被解析**：Stop 用 `{"decision":"block","reason":...}` 或 `{"continue":false,"stopReason":...}`；PreToolUse 也可用 `{"permissionDecision":"allow"|"deny","reason":...}` 做更结构化决策（exit 0）。本计划以 exit 2 为主、JSON 为辅，**Stop 必须用 JSON**。
    - `$CLAUDE_PROJECT_DIR` 作为 path placeholder（`${CLAUDE_PROJECT_DIR}/...`）与**实际环境变量**两种形式都被支持；命令字符串里**用双引号**避免 `$` 提前被 shell 展开。
    - **热重载即生效**，无 restart/无审批——代理写 `.claude/settings.json` 之后自己的下一次工具调用就按新 hook 跑。
- [Claude Code Settings](https://code.claude.com/docs/en/settings.md)
  - 具体章节：`.claude/settings.json`（项目共享、纳入 git）vs `.claude/settings.local.json`（个人本地、gitignore）vs `~/.claude/settings.json`（全局本地）；hooks 块结构。
  - 原因：确认本功能写 `settings.json` 而非 `settings.local.json`。**注意**当前仓库的 `.claude/` 已有 `settings.local.json` 但**没有** `settings.json`（已通过 `ls -la` 确认）——直接新建 `settings.json` 即可。
- [gofmt 文档](https://pkg.go.dev/cmd/gofmt)
  - 具体章节：`-l` 标志（列出格式不符的文件，符合则无输出）。
  - 原因：`gofmt-check.py` 用 `gofmt -l <file>` 判断是否需要格式化。

### 需要遵循的模式

**hook stdin/退出码契约（所有脚本统一）：**

```python
# .claude/hooks/_common.py 提供的范式
import json, sys

def read_event():
    """从 stdin 读取并解析 Claude Code hook JSON；解析失败时放行（退出 0）以免误伤。"""
    try:
        return json.load(sys.stdin)
    except Exception:
        sys.exit(0)  # 输入异常时不拦截，避免阻断正常工作

def deny(reason: str):
    """阻断本次工具调用：stderr 写原因 + 退出码 2。"""
    print(reason, file=sys.stderr)
    sys.exit(2)

def allow():
    sys.exit(0)
```

**路径判定模式（与 `.gitignore` 对齐，避免误判）：**

```python
# 私有/生成目录前缀（相对项目根）。注意 .env 要放行 .env.example
PRIVATE_PREFIXES = ("reports/", "reports.backup-", ".cache/", "logs/",
                    "outputs/", ".gocache", ".gomodcache/")
def is_private(rel_path: str) -> bool:
    p = rel_path.lstrip("./")
    if p.startswith(".env") and not p.startswith(".env.example"):
        return True
    return any(p.startswith(pre) for pre in PRIVATE_PREFIXES)
```

**命名约定：** hook 脚本用 kebab-case + 动词前缀（`block-*`、`guard-*`、`gofmt-check`、`remind-*`），与 `scripts/` 下既有脚本风格一致。共享模块用下划线前缀 `_common.py` 表示内部。

**隐私模式（强约束，来自 AGENTS.md）：** hook 脚本**绝不打印**任何聊天派生内容/secret/key/prompt。拦截原因里只写**路径、命令模式名、规则编号**。例如 deny 文案：`Blocked: write to private/generated path 'reports/x.md' (AGENTS.md privacy rule). Use a non-ignored path or ask the user.`——只含路径，不含文件内容。

**反模式（必须避免）：**
- ❌ 不要在 hook 里 `cat`/打印被操作文件的内容。
- ❌ 不要让 hook 解析失败就退出码 2（会无差别阻断所有工具调用，导致代理彻底卡死）；解析失败一律放行（退出 0）。
- ❌ `Stop` hook 不要无条件退出码 2（会造成「无法停止」死循环）；必须有自终止条件。
- ❌ 不要覆盖既有 `.claude/settings.json`；若存在要 merge `hooks` 键。
- ❌ 不要把 hooks 写进 `settings.local.json`（那是个人本地、被 gitignore，无法对团队/其它会话生效）。

---

## 实现计划

### 阶段 1：基础准备

建立 `.claude/hooks/` 目录与共享工具模块，确认 hook stdin 契约可用。先把「读 JSON → 判定 → 退出码」的最小骨架跑通并单测，再写具体规则。

**任务：** 创建目录；创建 `_common.py`；用 mock JSON 验证 `read_event`/`deny`/`allow` 行为。

### 阶段 2：核心实现（逐个 hook 脚本，各自独立可测）

按风险优先级实现 5 个强制 hook + 1 个可选 hook。每个脚本写完**立即**用 `test_hooks.sh` 里的对应正/负样例验证退出码，再进下一个。

**任务：** 依次实现 `block-private-writes`、`block-batch-delete`、`block-report-commit`、`guard-quota-commands`、`gofmt-check`、`remind-state-update`。

### 阶段 3：集成（接线到 settings.json）

把脚本挂到 `.claude/settings.json` 的对应事件上。先确认 `settings.json` 是否存在，存在则 merge，不存在则新建。用 `python3 -c "import json"` 校验 JSON 合法。

**任务：** 创建/合并 `settings.json`；JSON 合法性校验；（人工）在真实 Claude Code 会话里触发一次拦截做端到端确认。

### 阶段 4：测试与验证

完善 `test_hooks.sh` 覆盖所有 hook 的正样例（应放行，退出 0）与负样例（应拦截，退出 2）；编写 `docs/claude-code-hooks.md`；更新 `progress.md` / `session-handoff.md` / `feature_list.json`（DoD）。

**任务：** 补全单测；写文档；更新状态文件。

---

## 分步任务

重要：严格按顺序执行所有任务，从上到下。每个任务都必须是原子性的，并且可独立测试。**做完一步先跑它的 VALIDATE，通过再进下一步。**

### 任务格式指南

- **CREATE**: 创建新文件或新组件
- **UPDATE**: 修改现有文件
- **ADD**: 向现有代码中插入新功能
- **MIRROR**: 复用代码库其他位置的模式

---

### 任务 1 — CREATE `.claude/hooks/` 目录

- **IMPLEMENT**: 创建空目录 `.claude/hooks/`。
- **PATTERN**: 与 `scripts/` 同级思路，但放在 `.claude/` 命名空间内以区别于产品 `scripts/` 和 app `messagehook`。
- **IMPORTS**: 无。
- **GOTCHA**: 不要叫 `.claude/scripts`，避免与 `scripts/ralph` 混淆；不要叫 `hooks/`（根目录），避免与 git hooks（`.git/hooks`）混淆。
- **VALIDATE**: `mkdir -p .claude/hooks && test -d .claude/hooks && echo OK`

---

### 任务 2 — CREATE `.claude/hooks/_common.py`

- **IMPLEMENT**: 提供 `read_event()`、`deny(reason)`、`allow()`、`is_private(rel_path)`、以及把绝对路径转为「相对项目根」的 `rel_to_project(path)`（用 `os.environ.get("CLAUDE_PROJECT_DIR")` 或 `cwd`，取不到时回退到 `os.getcwd()`）。`read_event` 解析失败时 `sys.exit(0)`（放行）。
- **PATTERN**: 见上文「hook stdin/退出码契约」与「路径判定模式」代码块。
- **IMPORTS**: `import json, sys, os`。
- **GOTCHA**: 解析失败必须放行（退出 0），不能退出 2。`is_private` 要放行 `.env.example`。路径统一用 `/` 比较前先 `replace("\\\\", "/")` 以兼容 Windows。
- **VALIDATE**:
  ```bash
  python3 - <<'PY'
  import importlib.util, sys
  spec = importlib.util.spec_from_file_location("c", ".claude/hooks/_common.py")
  c = importlib.util.module_from_spec(spec); spec.loader.exec_module(c)
  assert c.is_private("reports/x.md") is True
  assert c.is_private(".cache/y") is True
  assert c.is_private(".env") is True
  assert c.is_private(".env.example") is False
  assert c.is_private("internal/chatlog/foo.go") is False
  print("OK")
  PY
  ```

---

### 任务 3 — CREATE `.claude/hooks/block-private-writes.py`

- **IMPLEMENT**: 读事件 → 取 `tool_input.file_path`（Write/Edit/MultiEdit 都用此字段）→ 转相对路径 → 若 `is_private` **且** 路径不在 `.claude/hooks/` 或 `.claude/settings.json` 下 → `deny`，否则 `allow`。deny 文案：`Blocked: write to private/generated path '<rel>' is forbidden by AGENTS.md privacy rules (.gitignore'd). Choose a tracked path or ask the user.` 必须**显式放行** `.claude/hooks/*.py` 和 `.claude/settings.json`——否则实现者后续 Edit/Write 这些文件时会被自己的 hook 阻断，造成自死锁（**这是热重载无安全门的关键风险**，见方案陈述「关键风险声明」第 1 条）。
- **PATTERN**: MIRROR `_common.py` 的 `is_private`/`deny`/`allow`。
- **IMPORTS**: `from _common import ...`（用 `sys.path.insert(0, os.path.dirname(__file__))` 确保能 import 同目录 `_common`）。
- **GOTCHA**: `tool_input` 可能缺 `file_path`（如某些工具变体），缺失时放行。MultiEdit 也用 `file_path`。`.claude/commands/`、`.claude/skills/` 也应放行（不是私有，但合理）——本计划**只**放行 `.claude/hooks/` 和 `.claude/settings.json`（最小放行集），其它 `.claude/` 写入若想保护，加 path-level allowlist。
- **VALIDATE**:
  ```bash
  # 写入私有目录应被拦截
  echo '{"tool_name":"Write","tool_input":{"file_path":"reports/a.md"}}' | python3 .claude/hooks/block-private-writes.py; echo "deny_exit=$?"   # 期望 2
  echo '{"tool_name":"Write","tool_input":{"file_path":"internal/chatlog/x.go"}}' | python3 .claude/hooks/block-private-writes.py; echo "allow_exit=$?"  # 期望 0
  echo '{"tool_name":"Write","tool_input":{"file_path":".env.example"}}' | python3 .claude/hooks/block-private-writes.py; echo "envexample_exit=$?"  # 期望 0
  # 自修改放行（关键！避免自死锁）
  echo '{"tool_name":"Edit","tool_input":{"file_path":".claude/hooks/block-private-writes.py"}}' | python3 .claude/hooks/block-private-writes.py; echo "selfedit_exit=$?"  # 期望 0
  echo '{"tool_name":"Write","tool_input":{"file_path":".claude/settings.json"}}' | python3 .claude/hooks/block-private-writes.py; echo "selfcfg_exit=$?"  # 期望 0
  ```

---

### 任务 3.5 — CREATE `.claude/hooks/block-private-reads.py`

- **IMPLEMENT**: 读事件 → 取 `tool_input.command`（Bash）→ 检测「读取命令」+「路径指向私有目录」的组合 → 命中则 `deny`。「读取命令」匹配（任一）：`\b(cat|head|tail|less|more|strings|xxd|od)\s+`、`\b(awk|sed)\b.*<\s*(reports|reports\.backup|\.cache|logs|outputs|\.env)`、`\bjq\b.*\s+(reports|reports\.backup|\.cache|logs|outputs|\.env)`、`\bsqlite3?\b.*\s+(reports|reports\.backup|\.cache|logs|outputs)`。deny 文案：`Blocked: reading private/generated content via '<reader>' is forbidden by AGENTS.md:112. Verify by path/metadata only.`
- **PATTERN**: MIRROR `_common.py` + 正则。
- **IMPORTS**: `import re` + `from _common import is_private, deny, allow, read_event`。
- **GOTCHA**: **必须**放行 `ls -la`、`wc -l`、`stat`、`file`、`du`、`jq '.meta' report.json`、`grep -c`（元数据）——只对真正把内容 dump 出来的命令拦截。对 `grep`（无 flag 或 `-r/-n`）保守拦截，agent 转用 `grep -c` 即可放行。`cat` 后跟项目代码路径（`internal/...`）放行。`open` 是 macOS GUI 工具，可不拦截。
- **VALIDATE**:
  ```bash
  echo '{"tool_name":"Bash","tool_input":{"command":"cat reports/daily.md"}}' | python3 .claude/hooks/block-private-reads.py; echo "cat=$?"            # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"head -n 5 .env"}}' | python3 .claude/hooks/block-private-reads.py; echo "headenv=$?"      # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"tail -f logs/x.log"}}' | python3 .claude/hooks/block-private-reads.py; echo "tail=$?"        # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"ls -la reports/"}}' | python3 .claude/hooks/block-private-reads.py; echo "ls=$?"             # 期望 0
  echo '{"tool_name":"Bash","tool_input":{"command":"wc -l reports/x.md"}}' | python3 .claude/hooks/block-private-reads.py; echo "wc=$?"            # 期望 0
  echo '{"tool_name":"Bash","tool_input":{"command":"grep error logs/x.log"}}' | python3 .claude/hooks/block-private-reads.py; echo "grep=$?"   # 期望 2（保守）
  echo '{"tool_name":"Bash","tool_input":{"command":"cat internal/chatlog/x.go"}}' | python3 .claude/hooks/block-private-reads.py; echo "gocat=$?"  # 期望 0
  ```

---

### 任务 4 — CREATE `.claude/hooks/block-batch-delete.py`

- **IMPLEMENT**: 读事件 → 取 `tool_input.command`（Bash）→ 用正则匹配批量删除模式 → 命中则 `deny`，否则 `allow`。匹配模式（任一命中即拦截）：`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*\s|-[a-zA-Z]*f[a-zA-Z]*\s).*` （`rm -r` / `rm -rf` / `rm -fr`）、`\bfind\b.*-delete\b`、`\bfind\b.*-exec\s+rm\b`、`\bxargs\b.*\brm\b`、`\bgit\s+clean\s+-[a-zA-Z]*f`、含通配符的 `rm`（`\brm\s+[^|;&]*[\*]`）。deny 文案：`Blocked: batch/recursive delete detected. AGENTS.md forbids script-based batch deletion; delete one file at a time and explain risk/rollback, or ask the user.`
- **PATTERN**: MIRROR `_common.py`。正则用 `re.search`，对每个模式逐一检查。
- **IMPORTS**: `import re` + `from _common import ...`。
- **GOTCHA**: 不要拦截单文件 `rm foo.txt`（AGENTS.md 允许一次删一个）。注意 `command` 可能是多行/带 `&&` 的复合命令，逐行或整体 search 均可，整体 search 更稳。允许 `rm -f singlefile`（单个 `-f` 无 `-r` 且无通配符）——所以 `rm -f` 不应单独命中：把模式收紧为「含 `r`（递归）」或「含通配符」或「find/xargs 批删」。重新核对：`rm -f a.txt` 不含 r、不含 `*` → 不命中，正确。
- **VALIDATE**:
  ```bash
  echo '{"tool_name":"Bash","tool_input":{"command":"rm -rf build/"}}' | python3 .claude/hooks/block-batch-delete.py; echo "rmrf=$?"      # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"find . -name \"*.tmp\" -delete"}}' | python3 .claude/hooks/block-batch-delete.py; echo "find=$?"  # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"rm logs/*.log"}}' | python3 .claude/hooks/block-batch-delete.py; echo "glob=$?"      # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"rm -f one.txt"}}' | python3 .claude/hooks/block-batch-delete.py; echo "single=$?"    # 期望 0
  echo '{"tool_name":"Bash","tool_input":{"command":"ls -la"}}' | python3 .claude/hooks/block-batch-delete.py; echo "ls=$?"              # 期望 0
  ```

---

### 任务 5 — CREATE `.claude/hooks/block-report-commit.py`

- **IMPLEMENT**: 读事件 → 取 `tool_input.command` → 仅处理含 `git add` 或 `git commit -a` 的命令 → 若命令显式包含私有目录名（`reports`、`reports.backup`、`.cache`、`logs`、`outputs`、`.env`）或使用全量暂存（`git add .`、`git add -A`、`git add --all`、`git commit -a`/`-am`）则 `deny`。deny 文案：`Blocked: this git command may stage private/generated paths (reports/, .cache/, logs/, outputs/, .env). Stage explicit tracked files instead, e.g. 'git add <file>'.`
- **PATTERN**: MIRROR `_common.py` + 正则。
- **IMPORTS**: `import re` + `from _common import ...`。
- **GOTCHA**: 放行显式安全暂存（`git add internal/chatlog/x.go`）。`git commit -a` 会自动暂存所有已跟踪改动——因为私有目录已被 gitignore，`-a` 其实不会暂存被忽略文件；但为防止 `.env` 这类已跟踪敏感文件，仍拦截 `-a` 全量提交并要求显式路径。`git status` / `git diff` 等只读命令必须放行。
- **VALIDATE**:
  ```bash
  echo '{"tool_name":"Bash","tool_input":{"command":"git add ."}}' | python3 .claude/hooks/block-report-commit.py; echo "addall=$?"        # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"git add reports/x.md"}}' | python3 .claude/hooks/block-report-commit.py; echo "addrep=$?"  # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"git add internal/chatlog/x.go"}}' | python3 .claude/hooks/block-report-commit.py; echo "addok=$?"  # 期望 0
  echo '{"tool_name":"Bash","tool_input":{"command":"git status"}}' | python3 .claude/hooks/block-report-commit.py; echo "status=$?"        # 期望 0
  ```

---

### 任务 5.5 — CREATE `.claude/hooks/block-destructive-git.py`

- **IMPLEMENT**: 读事件 → 取 `tool_input.command` → 若匹配破坏性 git 模式则 `deny`。**正则黑名单**（按 AGENTS.md:193 + :127 收紧；`\\bgit\\b` 后接子命令，整词匹配；`-` 与 `--` flag 用 `\\s+(-[a-zA-Z]{1,2}|--[a-z-]+)`）：
  - `\\bgit\\b\\s+commit\\b`：直接 `git commit`（Ralph 通过自己的 subagent 走 commit；当前 agent 直接 commit 即违规）。
  - `\\bgit\\b\\s+push\\b` 任意 ref/spec。
  - `\\bgit\\b\\s+(reset|rebase|restore|clean|rm)\\b`：含 `git reset`、`git rebase`、`git restore`、`git clean -f`/`-fd`、`git rm -r`。
  - `\\bgit\\b\\s+merge\\b`：直接 merge（除非 sub-shell 里是 `git merge --abort`——放行 `--abort`）。
  - `\\bgit\\b\\s+(branch|switch|checkout)\\b`：创建/切换/检出分支。
  - **特殊**：检测 `git checkout -- <path>`、`git restore <path>`、`git reset -- <path>` —— 这是「覆盖已修改的 dirty 文件」的模式（违反 AGENTS.md:127）。若 `<path>` 出现在 `git status --porcelain` 输出中，**额外**拦截；否则放行（agent 在 clean 路径上 restore 是合法操作）。实现：先 `subprocess.run(["git","status","--porcelain"], capture_output=True, text=True)` 取出 dirty 文件集，再判定命令中提及的路径是否在 dirty 集中。deny 文案：`Blocked: destructive git command '<cmd>' is forbidden by AGENTS.md:193 unless the user explicitly asked. If the user asked, ask them to run it themselves or use scripts/ralph/ralph.py for branch lifecycle.` / dirty-revert 文案：`Blocked: '<path>' is in the dirty set (AGENTS.md:127 — pre-existing dirty files belong to the user/other run; do not revert).`
- **PATTERN**: MIRROR `_common.py` + 正则 + `subprocess.run`（调 `git status`）。**注意** `ralph.py` 通过 `subprocess.run` 直接调 git 时**不会**进入本 hook（hook 只看 Bash tool），见方案陈述「关键风险声明」第 3 条——本 hook 仅拦截当前 agent 在 Bash tool 中的直接 git 调用。
- **IMPORTS**: `import re, subprocess` + `from _common import ...`。
- **GOTCHA**: 放行 `git status`、`git log`、`git diff`、`git show`、`git rev-parse`、`git fetch`（只读）。`git rev-parse` 是 ralph.py 用来读当前分支的（`ralph.py:191,201`）——必须放行。`git merge --abort` 放行。复合命令（`git commit -m 'x' && git push`）整串匹配任一模式即拦。dirty-set 检测会调 `git status`——在 non-git 目录里应回退到「不检测」并放行。
- **VALIDATE**:
  ```bash
  echo '{"tool_name":"Bash","tool_input":{"command":"git commit -m x"}}' | python3 .claude/hooks/block-destructive-git.py; echo "commit=$?"  # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"git push origin main"}}' | python3 .claude/hooks/block-destructive-git.py; echo "push=$?"  # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"git reset --hard HEAD~1"}}' | python3 .claude/hooks/block-destructive-git.py; echo "reset=$?"  # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"git rebase -i HEAD~3"}}' | python3 .claude/hooks/block-destructive-git.py; echo "rebase=$?"  # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"git merge --no-ff feature"}}' | python3 .claude/hooks/block-destructive-git.py; echo "merge=$?"  # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"git merge --abort"}}' | python3 .claude/hooks/block-destructive-git.py; echo "mergeabort=$?"  # 期望 0
  echo '{"tool_name":"Bash","tool_input":{"command":"git checkout -b foo"}}' | python3 .claude/hooks/block-destructive-git.py; echo "newbranch=$?"  # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"git status"}}' | python3 .claude/hooks/block-destructive-git.py; echo "status=$?"  # 期望 0
  echo '{"tool_name":"Bash","tool_input":{"command":"git rev-parse --abbrev-ref HEAD"}}' | python3 .claude/hooks/block-destructive-git.py; echo "revparse=$?"  # 期望 0
  echo '{"tool_name":"Bash","tool_input":{"command":"git log --oneline -5"}}' | python3 .claude/hooks/block-destructive-git.py; echo "log=$?"  # 期望 0
  # dirty-revert 检测（需要先有 dirty 文件，仓库里 pre-existing dirty 是 AGENTS.md CLAUDE.md）
  echo '{"tool_name":"Bash","tool_input":{"command":"git checkout -- CLAUDE.md"}}' | python3 .claude/hooks/block-destructive-git.py; echo "revertdirty=$?"  # 在本仓库运行应期望 2（CLAUDE.md 是 dirty）
  ```

---

### 任务 6 — CREATE `.claude/hooks/guard-quota-commands.py`

- **IMPLEMENT**: 读事件 → 取 `tool_input.command` → 若匹配配额/隐私敏感命令则 `deny`。匹配模式：`report\s+daily.*--vision`、`report\s+daily.*--summary`、`/api/v1/semantic/test`、`semantic\s+test`。deny 文案：`Blocked by default: '<matched>' consumes model quota / exposes prompts (AGENTS.md). Only run when the user explicitly requests it; ask for confirmation first.`
- **PATTERN**: MIRROR `_common.py` + 正则。
- **IMPORTS**: `import re` + `from _common import ...`。
- **GOTCHA**: 放行不带 `--vision`/`--summary` 的普通 `chatlog report daily`、`go run . report daily --help`（含 `--help` 应放行——加一条：若命令含 `--help` 直接放行）。这是「默认拦截、显式授权放行」策略；代理被拦后应转而询问用户，而不是绕过。
- **VALIDATE**:
  ```bash
  echo '{"tool_name":"Bash","tool_input":{"command":"chatlog report daily --vision"}}' | python3 .claude/hooks/guard-quota-commands.py; echo "vision=$?"   # 期望 2
  echo '{"tool_name":"Bash","tool_input":{"command":"go run . report daily --help"}}' | python3 .claude/hooks/guard-quota-commands.py; echo "help=$?"     # 期望 0
  echo '{"tool_name":"Bash","tool_input":{"command":"chatlog http list"}}' | python3 .claude/hooks/guard-quota-commands.py; echo "list=$?"               # 期望 0
  ```

---

### 任务 7 — CREATE `.claude/hooks/gofmt-check.py`（PostToolUse；语义：仅 stderr 提醒，不能 block）

- **IMPLEMENT**: 读事件 → 取 `tool_input.file_path` → 若以 `.go` 结尾 → 运行 `gofmt -l <file>`（`subprocess.run([..., "-l", file], capture_output=True, text=True)`）→ 若 stdout 非空（文件未格式化）则**写 stderr** 并 `sys.exit(2)`。**关键语义**：PostToolUse 下退出码 2 不会撤销已写入的文件（工具已执行），只把 stderr 反馈给 Claude 作「事后提醒」——这是 Claude Code 的硬约束（docs.claude.com hooks.md Exit code 表）；本 hook 用退出 2 是为了「大声」触发 stderr 反馈路径，若用退出 0 + 仅 stderr 也能起提醒作用，但退出 2 语义更明确。**不要**误以为退出 2 会阻止文件落地。deny 文案：`Reminder (PostToolUse cannot block): '<file>' is not gofmt-formatted. Run: gofmt -w <file> (then re-run ./init.sh).` 非 `.go` 文件、`gofmt` 不存在（`FileNotFoundError`）或文件已被格式化（`gofmt -l` 无输出）时放行（退出 0）。
- **PATTERN**: MIRROR `scripts/chatlog-ha-guard.sh` 的「调用外部命令 + 判断输出」风格。
- **IMPORTS**: `import subprocess, os` + `from _common import ...`。
- **GOTCHA**: `gofmt -l` 对已格式化文件**无输出且退出 0**；对未格式化文件输出文件名。`subprocess.run` 包装 try/except `FileNotFoundError`（无 gofmt）放行。**不要**用 `os.system`/`shell=True`（路径含空格会爆）。PostToolUse 退出 2 是「反馈」非「阻断」——若需要硬保证 gofmt 落地，应用 pre-commit git hook 或 CI（不在本计划范围）。
- **VALIDATE**:
  ```bash
  # 造一个未格式化的 .go 临时文件做正样例
  printf 'package x\nfunc F( ){\n}\n' > /tmp/_unfmt.go
  echo "{\"tool_name\":\"Write\",\"tool_input\":{\"file_path\":\"/tmp/_unfmt.go\"}}" | python3 .claude/hooks/gofmt-check.py; echo "unfmt=$?"  # 期望 2（stderr 含 reminder）
  gofmt -w /tmp/_unfmt.go
  echo "{\"tool_name\":\"Write\",\"tool_input\":{\"file_path\":\"/tmp/_unfmt.go\"}}" | python3 .claude/hooks/gofmt-check.py; echo "fmt=$?"    # 期望 0
  echo '{"tool_name":"Write","tool_input":{"file_path":"README.md"}}' | python3 .claude/hooks/gofmt-check.py; echo "md=$?"                    # 期望 0
  # FileNotFoundError 路径：临时 PATH 删 gofmt 不易，改为用一个不存在的可执行文件路径不可行（gofmt 走 PATH）
  # 可跳过此项，依赖 `gofmt` 必然存在（Go 1.24 工具链）
  rm -f /tmp/_unfmt.go
  ```

---

### 任务 8 —（可选）CREATE `.claude/hooks/remind-state-update.py`（Stop；带自终止计数器）

- **IMPLEMENT**: 读事件（`hook_event_name == "Stop"`）→ 取出 `session_id`（事件 JSON 中存在）→ 写/读侧计数文件 `$CLAUDE_PROJECT_DIR/.claude/hooks/.stop_block_count.<sha256(session_id)[:12]>`（用 session_id 哈希避免跨会话串扰；首次运行创建文件，计数 = 1）：
  - **若计数 ≥ 2**：Claude Code **无内置 Stop 循环防护**（docs.claude.com hooks.md Stop section 已确认），必须自终止。stdout 写 `{"continue": false, "stopReason": "remind-state-update: reached max block count; please update progress.md/session-handoff.md before truly ending."}` 并退出 0。这强制停止会话，让 agent 跳出循环。
  - **若计数 < 2** 且「git status --porcelain」显示存在已修改/新增的 `*.go` 文件**且** `progress.md` 不在 dirty 列表 → stdout 写 `{"decision": "block", "reason": "Definition of Done: you changed .go files but progress.md is not updated. Update progress.md/session-handoff.md before finishing, or say why it's skipped."}`，退出 0；同时把计数写为上一值 +1。
  - **若计数 < 2** 且 dirty 集中有 `progress.md`（或 dirty 集无 `.go`）→ stdout 写 `{}` 并退出 0（放行）。当 `progress.md` 出现于 dirty 列表时，自终止条件命中，agent 补完 DoD 后即可正常 stop。
- **PATTERN**: MIRROR `_common.py`；Stop hook 用 stdout JSON 而非退出码 2（Stop 事件阻断走 JSON 决策更可控，docs.claude.com Stop section）。
- **IMPORTS**: `import subprocess, json, os, hashlib` + `from _common import read_event`。
- **GOTCHA**:
  - **死循环风险**：自终止条件是「计数 ≥ 2 → 强制 `continue: false`」，确保最多阻断 1 次后无论 progress.md 是否更新都会终止会话。这是 Claude Code 文档明确无内置防护的必备自保逻辑。
  - 侧计数文件路径必须在项目根下（`$CLAUDE_PROJECT_DIR/.claude/hooks/`），不要写到 `/tmp`（会被系统清理）。`.claude/hooks/` 下的临时文件**不在**隐私目录黑名单内（已 `block-private-writes` 放行 `.claude/hooks/`），所以这种 .stop_block_count.* 文件可被 hook 安全写入。
  - 阻塞写法**必须是** stdout JSON 块（exit 0），不能依赖退出码 2——Stop 事件对退出码 2 的处理可能不一致。
  - **本任务可选**：若实现者评估侧计数文件的可靠性不足（多 agent 并发、文件系统权限等），可**整任务跳过**，在 `settings.json` 中**不挂载** Stop 块，只交付任务 3-7 + 3.5 + 5.5 共 7 个 Pre/Post hook。
- **VALIDATE**:
  ```bash
  # 最小化测试：仅在仓库根运行一次，记录 session_id 后清理计数文件
  cd /Volumes/WorkSSD/Dev/chatlog_alpha
  rm -f .claude/hooks/.stop_block_count.*  # 清环境
  echo '{"hook_event_name":"Stop","session_id":"test-sess-1"}' | python3 .claude/hooks/remind-state-update.py; echo "first_exit=$?"  # 期望：dirty 有 .go（AGENTS.md/CLAUDE.md 是 .md 不算）+ progress.md 未改 → exit 0，stdout 含 "block"
  echo '{"hook_event_name":"Stop","session_id":"test-sess-1"}' | python3 .claude/hooks/remind-state-update.py; echo "second_exit=$?"  # 期望：计数=2 → stdout 含 "continue\": false
  echo '{"hook_event_name":"Stop","session_id":"test-sess-1"}' | python3 .claude/hooks/remind-state-update.py; echo "third_exit=$?"  # 期望：计数已≥2 → 继续强制停止
  rm -f .claude/hooks/.stop_block_count.*  # 清理
  ```

---

### 任务 9 — CREATE `.claude/settings.json`（接线）

- **IMPLEMENT**: 新建 `.claude/settings.json`，挂载所有 hooks。命令统一用 `python3 "$CLAUDE_PROJECT_DIR/.claude/hooks/<name>.py"`（注意 `$CLAUDE_PROJECT_DIR` 用双引号包裹整个命令以保留扩展）。结构：
  ```json
  {
    "hooks": {
      "PreToolUse": [
        { "matcher": "Write|Edit|MultiEdit",
          "hooks": [ { "type": "command", "command": "python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/block-private-writes.py\"" } ] },
        { "matcher": "Bash",
          "hooks": [
            { "type": "command", "command": "python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/block-private-reads.py\"" },
            { "type": "command", "command": "python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/block-batch-delete.py\"" },
            { "type": "command", "command": "python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/block-destructive-git.py\"" },
            { "type": "command", "command": "python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/block-report-commit.py\"" },
            { "type": "command", "command": "python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/guard-quota-commands.py\"" }
          ] }
      ],
      "PostToolUse": [
        { "matcher": "Write|Edit|MultiEdit",
          "hooks": [ { "type": "command", "command": "python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/gofmt-check.py\"" } ] }
      ],
      "Stop": [
        { "hooks": [ { "type": "command", "command": "python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/remind-state-update.py\"" } ] }
      ]
    }
  }
  ```
- **PATTERN**: 见上文 settings 结构。**注意** `Stop` 事件不支持 `matcher` 字段（docs.claude.com hooks.md Matcher field table）——故 `Stop` 块直接放 `hooks` 数组，无 matcher 键。Pre/Post 块用 `matcher`。
- **IMPORTS**: 无。
- **GOTCHA**:
  - **若 `.claude/settings.json` 已存在，绝不能覆盖**——先 Read，再 merge `hooks` 键。**当前确认 settings.json 不存在**（`ls -la .claude/settings.json` 报 No such file），故本任务直接新建。
  - `Stop` hook 为可选；若任务 8 跳过则删掉 `Stop` 块。**单 hook 整块缺日志**——若以后想临时关闭单个 hook，正确做法是 `if`-permission rule 而非从 settings 删（删除会让同 matcher 下其它 hook 顺序也变）。
  - 不要写进 `settings.local.json`（那是个人本地、被 gitignore，团队其它代理拿不到）。
  - **热重载即生效**——写完本文件后，**当前会话**的后续 PreToolUse 已经按新 hook 跑了。本计划已在 `block-private-writes` 任务中放行 `.claude/hooks/*.py` 与 `.claude/settings.json` 自路径，避免自死锁；其它 hook 自修改时同样需要这些路径可写（本计划不强制要求，依赖 Write tool 不被 block-private-writes 拦；其它 Pre/Bash hook 的 .py 文件不在 Bash 路径上所以不触发）。
- **VALIDATE**: `python3 -c "import json; json.load(open('.claude/settings.json')); print('valid json')"`

---

### 任务 10 — CREATE `.claude/hooks/test_hooks.sh`（汇总单测）

- **IMPLEMENT**: 把任务 2-7 + 3.5 + 5.5 的 VALIDATE 正/负样例汇总成一个脚本，每条断言期望退出码（PreToolUse 期望 0 或 2），全部通过则末尾打印 `ALL HOOK TESTS PASSED`，任一失败则 `exit 1` 并打印失败项。用 `set -u`；每条用 `actual=$?; [ "$actual" = "2" ] || { echo "FAIL ..."; fail=1; }` 形式。**总条数约 35+ 个断言**（8 个 hook × 平均 4-5 条）。
- **PATTERN**: MIRROR `scripts/ralph/test_branch_merge.sh` 的 sandbox+断言风格（但本脚本无需建 git sandbox，纯喂 JSON）。
- **IMPORTS**: 无（纯 bash + python3 调用）。
- **GOTCHA**:
  - 临时 `.go` 文件用 `/tmp` 并在末尾清理。`block-destructive-git` 的 dirty-revert 测试需要清空 dirty 集后才能稳定复现——可改为「mock git status 输出」的方式，或在 test 末尾 cleanup。**最稳的策略**：在 test 脚本开头 `git stash --include-untracked` 保存当前 dirty，结尾 `git stash pop` 还原；如 stash 失败则跳过 dirty 测试。**本仓库当前有 dirty 文件**（AGENTS.md / CLAUDE.md）——`git checkout -- CLAUDE.md` 的 dirty-revert 测试会真实命中并拦截，**这正是预期行为**，断言为 2。
  - `block-destructive-git` 会调 `git status` 真实子进程——需要仓库根目录运行（test 脚本开头 `cd` 到 `$PROJECT_DIR`）。
  - `remind-state-update` 的 sidecar 计数文件 `.stop_block_count.*` 必须 cleanup 否则污染后续运行（`rm -f .claude/hooks/.stop_block_count.*` 放 test 开头与末尾）。
- **VALIDATE**: `cd /Volumes/WorkSSD/Dev/chatlog_alpha && bash .claude/hooks/test_hooks.sh && echo HARNESS_OK`

---

### 任务 11 — CREATE `docs/claude-code-hooks.md`（文档）

- **IMPLEMENT**: 文档说明：每个 hook（8 个：6 PreToolUse + 1 PostToolUse + 1 可选 Stop）的触发事件/matcher/拦截规则/deny 文案；与产品 `messagehook`、`/api/v1/hook/*` 的区别声明；如何临时绕过（注释 settings.json 对应行——注意：删除单 hook 的副作用，必要时整块禁用）；如何离线测试（`bash .claude/hooks/test_hooks.sh`）；如何新增 hook；**「关键风险声明」5 条**（热重载、subprocess 不可拦截、Ralph 绕过、PostToolUse 不能 block、Stop 循环防护）必须出现在文档里供后续维护者参考。
- **PATTERN**: MIRROR `docs/daily-report.md` / `docs/graph-digest.md` 的结构与中文+English术语风格。
- **IMPORTS**: 无。
- **GOTCHA**: 文档开头**明确声明**这是 Claude Code 代理 hooks，不是产品消息钩子，避免后续维护者混淆。
- **VALIDATE**: `test -f docs/claude-code-hooks.md && grep -q "messagehook" docs/claude-code-hooks.md && grep -q "Ralph" docs/claude-code-hooks.md && grep -q "热重载\|hot.reload\|hot reload" docs/claude-code-hooks.md && echo DOC_OK`

---

### 任务 12 — UPDATE 状态文件（DoD）

- **IMPLEMENT**: 在 `feature_list.json` 记录本侧任务（feature id 形如 `claude-code-guard-hooks-2026-06-19`，含 evidence：`bash .claude/hooks/test_hooks.sh` 输出、settings.json 合法性）；在 `progress.md` 追加 What changed / Verification Evidence / Not Verified（端到端真实会话拦截未验证则标 `未验证`）/ Risks / Next；在 `session-handoff.md` 写可重启的 Next Session 路径。可在 `AGENTS.md` 的「Key Files」补一行指向 `docs/claude-code-hooks.md`（可选）。
- **PATTERN**: MIRROR `progress.md` 既有条目结构。
- **IMPORTS**: 无。
- **GOTCHA**: 不要提交私有报告；本任务只改文档/状态文件。`feature_list.json` 是 JSON，改完要校验合法。
- **VALIDATE**: `python3 -c "import json; json.load(open('feature_list.json')); print('flist ok')" && grep -q "guard-hooks" progress.md && echo STATE_OK`

---

## 测试策略

核心思路：**每个 hook 都是读 stdin JSON、按退出码表达决策的独立可执行**，因此无需启动 Claude Code，用「mock JSON → 检查退出码」即可完整单测。

### 单元测试

`test_hooks.sh` 对每个 hook 至少各 1 个**负样例（应拦截，退出 2）** + 1 个**正样例（应放行，退出 0）**，断言遵循各任务 VALIDATE 块。fixture（mock JSON）直接 echo 进 stdin，与项目「verify by status/exit code, not by content」的隐私原则一致。

### 集成测试

`settings.json` 的 JSON 合法性（`json.load`）+ hooks 块结构正确。真实端到端拦截（在 Claude Code 会话里实际触发一次 `Write reports/x.md` 看是否被 deny）属于**手动验证**，因为它需要交互式会话。

### 边界情况

- stdin 非法 JSON / 空 → 必须放行（退出 0），绝不阻断。
- `tool_input` 缺 `file_path` / `command` → 放行。
- `.env.example` → 放行（不是私有）。
- 单文件 `rm one.txt`（无 `-r`、无通配符）→ 放行。
- `git status` / `git diff` / `git rev-parse` / `git merge --abort` / `git log` → 放行。
- `report daily --help` → 放行。
- 已 gofmt 的 `.go` → 放行；无 `gofmt` 二进制 → 放行（不 crash）。
- Stop hook：`progress.md` 已改动 → 放行（自终止之一）；计数 ≥ 2 → 用 `continue:false` 强制停止（自终止之二）。
- `block-private-writes` 对 `.claude/hooks/*.py` 与 `.claude/settings.json` → 放行（避免自死锁；**关键**）。
- `block-private-reads` 对 `ls` / `wc` / `stat` / `du` / `cat <非私有路径>` → 放行。
- `block-destructive-git` 在 non-git 目录里 → `git status` 失败时回退到「不检测 dirty 集」、放行其它所有检查。
- 仓库内 `git checkout -- CLAUDE.md`（CLAUDE.md 是 dirty）→ 拦截（dirty revert 命中）。

---

## 验证命令

执行所有命令，确保零回归与功能 100% 正确。

### 级别 1：语法与风格

```bash
# 所有 hook 脚本语法可编译
for f in .claude/hooks/*.py; do python3 -m py_compile "$f" || echo "SYNTAX FAIL $f"; done
# settings.json 合法
python3 -c "import json; json.load(open('.claude/settings.json')); print('settings ok')"
# feature_list.json 合法
python3 -c "import json; json.load(open('feature_list.json')); print('flist ok')"
```

### 级别 2：单元测试

```bash
bash .claude/hooks/test_hooks.sh
```

### 级别 3：集成测试

```bash
# 确认未触碰 Go 产品代码（本功能应 0 改动 internal/ cmd/）
git status --porcelain internal/ cmd/ | grep . && echo "WARN: product code changed" || echo "no product code change"
# 仓库既有根 harness 不回归
node scripts/check-root-harness.mjs
```

### 级别 4：手动验证

```text
1. 在真实 Claude Code 会话里让代理尝试 Write 到 reports/_probe.md → 应被 block-private-writes 拦截并提示。
2. 让代理尝试 Bash `rm -rf /tmp/_probe_dir` → 应被 block-batch-delete 拦截。
3. 让代理尝试 Bash `git add .` → 应被 block-report-commit 拦截。
4. 让代理尝试 Bash `chatlog report daily --vision` → 应被 guard-quota-commands 拦截。
5. 让代理编辑一个故意不规范的 .go 文件 → gofmt-check 应反馈提醒。
（手动验证消耗一次交互会话；若未做须在 progress.md 标记「未验证」。）
```

### 级别 5：附加验证（可选）

```bash
# 用 init.sh 快速门确认仓库整体未被打破（不涉及运行时/配额）
./init.sh
```

---

## 验收标准

- [ ] `.claude/hooks/` 下 7 个强制 hook 脚本（`block-private-writes` / `block-private-reads` / `block-batch-delete` / `block-destructive-git` / `block-report-commit` / `guard-quota-commands` / `gofmt-check`）+ 可选 `remind-state-update` + `_common.py` 全部存在且 `py_compile` 通过。
- [ ] `bash .claude/hooks/test_hooks.sh` 全绿（每个 hook 正/负样例退出码符合预期，总 35+ 断言）。
- [ ] `.claude/settings.json` 合法且正确挂载 6 个 PreToolUse + 1 个 PostToolUse（+ 可选 1 个 Stop 无 matcher）。
- [ ] 0 改动 `internal/` 与 `cmd/`（不触碰产品代码）。
- [ ] 私有目录写入、`cat reports/...` / `head .env`、批量删除、`git commit` / `git push` / `git reset --hard` / `git merge` / `git checkout -- <dirty>`、`git add .`、配额命令在 mock 测试中均被拦截（退出 2）。
- [ ] 合法操作（写产品代码、`git status`、`--help`、单文件 rm、`.env.example`、`ls`/`wc`/`cat <非私有>`、`git rev-parse`）均放行（退出 0）。
- [ ] 非法/空 stdin 放行，不会误阻断。
- [ ] `block-private-writes` 对 `.claude/hooks/*.py` 与 `.claude/settings.json` 放行（防自死锁）。
- [ ] `docs/claude-code-hooks.md` 明确区分了本 hooks 与产品 `messagehook`/`/api/v1/hook/*`，并列出全部 5 条「关键风险声明」。
- [ ] `progress.md` / `session-handoff.md` / `feature_list.json` 按 DoD 更新，端到端手动验证如未做则标 `未验证`。
- [ ] `node scripts/check-root-harness.mjs` 不回归。

---

## 完成检查清单

- [ ] 所有任务均已按顺序完成，每步 VALIDATE 当场通过。
- [ ] `test_hooks.sh` 全绿。
- [ ] settings.json / feature_list.json JSON 合法。
- [ ] 未触碰 Go 产品代码（`git status internal/ cmd/` 干净）。
- [ ] 根 harness 不回归。
- [ ] 文档完成且含命名区分声明。
- [ ] 状态文件（progress/handoff/feature_list）更新。
- [ ] 手动端到端验证完成或显式标注未验证。

---

## 已知限制

明确记录本方案**不能**覆盖的攻击面，避免给人「装上 hooks 就高枕无忧」的错觉：

1. **子进程不可见**：hooks 只看顶层 `Bash` 命令字符串。代理写一段 `bash -c 'rm -rf x'` 提交给 Bash tool 时，hook 看到的是 `bash -c 'rm -rf x'` 整串（仍然能匹配 `rm -rf`），但若代理把 `rm -rf` 写进一个**脚本文件**然后 `Bash(command="./that-script.sh")`，hook 只看到 `./that-script.sh`，看不到脚本内容里的 `rm -rf`。本仓库 `init.sh` 干净（已验证），但实现者自己写的临时脚本可能成为绕过路径。**硬保证**需要外部 `if` permission rule 或 pre-commit git hook。
2. **PostToolUse 无法阻断**：`gofmt-check.py` 只能做事后提醒。代理写一个未格式化的 .go 文件后，hook 只能把 reminder 喂回给模型；模型可以选择忽略 reminder 直接提交。**硬保证 gofmt** 需要 pre-commit git hook + CI。
3. **Ralph git 路径绕过**：`scripts/ralph/ralph.py` 通过 `subprocess.run(["git", ...])` 直接调 git（`ralph.py:179-186`），**不经过**当前会话的 Bash tool，本计划的 `block-destructive-git` / `block-report-commit` 看不到。Ralph 的 git 纪律由 ralph.py 自身代码（`ralph.py:342,350,370,606` 的 allowlist）保证，**不在本计划范围**。若要硬保证，需修改 ralph.py 把 git 调用路由到一个受 hooks 验证的 wrapper 脚本——但那会引入 subprocess→hook→subprocess 链，工程代价大、价值低（ralph 已经是单点可控）。
4. **热重载无安全门**：代理在会话中**写** `.claude/settings.json` 后，**本会话**后续所有工具调用立即按新 hook 跑，没有 restart 或人工 review。极端情况：恶意/被劫持的 agent 写一段 hook 把所有 PreToolUse 重定向到它控制的命令。**缓解**：
   - `block-private-writes` 放行 `.claude/settings.json`（否则实现者无法修改配置），但**不放行**其他 agent 写入——这个信任根是「**当前 session 的用户**（不是 agent）首次确认 hooks 内容」。
   - 实现者应将本计划附上 PR 评审，让**人类 reviewer**检查 `.claude/settings.json` 内容后再 merge。
   - 极端风险下可设 `disableAllHooks: true`（Claude Code 全局禁 hooks）或使用企业 managed settings 锁死。
5. **Stop 循环防护不完美**：本计划用 `session_id` 哈希 + 侧计数文件做自终止（计数 ≥ 2 → `continue:false`）。但：
   - 若 agent 主动删除 `.claude/hooks/.stop_block_count.*` 计数文件 → 计数器失效、可能死循环。
   - 多个并发 session 用不同 session_id 互不干扰，单 session 内计数仍然有效。
   - 根本解决：人工 review 任务 8，决定是否挂载 Stop hook；若拒绝，本计划其余 7 个 Pre/Post hook 不受影响。
6. **跨平台路径**：`is_private` 用 `/` 前缀匹配，Windows 上 `Write("reports\\x.md")` 不会命中（`reports\\x.md` 不以 `reports/` 开头）。缓解：`_common.py` 的 `rel_to_project` 先做 `path.replace("\\\\", "/")`。**`is_private` 自身也要 normalize 输入**（已包含在 `_common.py` 设计中，task 2 验证用 `os.sep` 兼容测试用例）。
7. **Hook 并发与 stdout/stderr 竞态**：多 hook 并行运行时（matcher 块内 `hooks` 数组是并行的）独立 stdout/stderr，stderr 都喂回给模型时**顺序不保证**。本计划每个 hook 的 stderr 是独立的 deny 消息，混在一起模型仍能读懂；但若未来要在 hook 里输出大段上下文，需注意此限制。
8. **Privacy-by-metadata 不能完全自动**：本计划只能「阻止打印」/「强制 metadata-only verification」；但代理仍可**生成**含私有内容的报告文件（在 `reports/` 外）并提交。`block-private-writes` 只拦写入 `reports/`，对其它位置（如 `docs/something.md`）写含聊天内容的长篇报告不拦。**这是模型行为而非 hook 能力范围**——AGENTS.md 的隐私规则最终要靠 reviewer 监督代码 diff。

---

## 备注

**为什么是「Claude Code 代理 hooks」而不是别的 hooks？**
「hooks」一词在本仓库有三种可能含义：(a) 产品自身的 `internal/chatlog/messagehook` 消息钩子；(b) `/api/v1/hook/*` HTTP 端点；(c) Claude Code 代理工作流 hooks。本计划选 (c)，因为：当前 `.claude/` 只有 `settings.local.json`、零 hooks，而 `AGENTS.md` 有大量**未被强制执行**的硬规则，正是 hooks 的理想用武之地；且 (a)(b) 属于产品功能，改动需求未由用户提出，贸然改动违反「one feature / stay in scope」。本功能**零侵入产品代码**，纯增量、可独立测试、风险低、价值高。

**8 个 hook 的覆盖矩阵**：

| Hook | 事件 | Matcher | 拦截规则 | AGENTS.md 锚点 |
|---|---|---|---|---|
| `block-private-writes` | PreToolUse | Write\|Edit\|MultiEdit | 写 `reports/`、`.cache/`、`logs/`、`outputs/`、`.env` | L104, L112, L168 |
| `block-private-reads` | PreToolUse | Bash | `cat`/`head`/`tail`/`grep`/`jq`/`sqlite3` 读私有路径 | L112, L116-117 |
| `block-batch-delete` | PreToolUse | Bash | `rm -rf`、`find -delete`、`git clean -fd`、通配符删除 | L119 |
| `block-destructive-git` | PreToolUse | Bash | `git commit`/`push`/`reset`/`rebase`/`merge`/`branch`/`checkout`/`restore`、dirty-revert | L127, L167-170, L193 |
| `block-report-commit` | PreToolUse | Bash | `git add .`/`-A`、显式暂存私有目录 | L168, L193 |
| `guard-quota-commands` | PreToolUse | Bash | `report daily --vision/--summary`、`semantic test` | L81-86, L114 |
| `gofmt-check` | PostToolUse | Write\|Edit\|MultiEdit | 改 .go 后 `gofmt -l` 提醒 | L102（Go 工具链） |
| `remind-state-update`（可选）| Stop | （无 matcher） | 改了 .go 但 progress.md 未改 → 补 DoD | Definition of Done |

**设计取舍：**
- 用「退出码 2 + stderr」做 PreToolUse 阻断；PostToolUse 退出 2 仅反馈（无 block 能力）；Stop 走 JSON 决策；`continue:false` 强制停止做最后兜底。
- 用 `python3` 而非 `jq`/纯 bash：`settings.local.json` 已允许 `Bash(python3 *)`；Python 标准库 JSON 解析跨平台稳定（项目支持 macOS+Windows，Windows 上 `jq` 常缺失）。
- hooks 写 `settings.json`（纳入 git、团队共享）而非 `settings.local.json`（个人、gitignore）：规则应对所有在本仓库工作的代理生效。
- **自修改放行**：`block-private-writes` 显式放行 `.claude/hooks/*.py` 与 `.claude/settings.json`——这是热重载无安全门下的必要自保；不放行 → 实现者写完 hook 后无法继续 Edit 它们 → 自死锁。
- **Stop 自终止**：用 `session_id` 哈希 + 侧计数文件 + `continue:false` 强制停止三重防护，对抗 Claude Code 无内置循环防护的硬约束。
- **Ralph 路径不重复覆盖**：`block-destructive-git` 不覆盖 `subprocess.run(["git", ...])`——ralph.py 已是单点可控，重复覆盖会引入 subprocess→hook→subprocess 链，工程代价大于价值。
- **保守读取拦截**：`grep` 一律拦（无 flag）——`grep -c`/`-l`/`-q` 是元数据，agent 可改用后者，避免误让 `grep` dump 出聊天内容。

**未来可扩展（不在本期范围）：**
- `SessionStart` hook 自动注入 `progress.md`/`session-handoff.md` 摘要（类似 `prime` skill）。
- `PreToolUse` 拦截对 `temporal_graph.db` 的直接写入（配合 TODO-2026-06-10 WAL 迁移的备份前置）。
- 与 `scripts/chatlog-ha-guard.sh` 联动，在 hook 里做轻量健康检查。
- 把 `block-destructive-git` 扩展到 `wrangler` / `terraform` / `kubectl delete` 等外部 CLI 破坏性子命令（如果未来仓库用到）。
- pre-commit git hook + CI 双层硬保证 gofmt、隐私扫描。

**信心分数：7/10** —— 8 个 hook 逻辑均简单且每个都可离线单测，把把握从 8 降到 7 的因素：(1) 真实 Claude Code 端到端拦截行为依赖具体版本的退出码/matcher 语义，需手动确认一次；(2) Stop hook 的 `continue:false` JSON 决策需要真实 stop 流程验证；(3) `block-destructive-git` 的 dirty-revert 检测依赖 `git status` 真实输出，在 dirty 仓库里的稳定行为需实测；(4) 热重载场景下「自修改放行」是否会引入新的攻击面需在 PR 评审中讨论；(5) `block-private-reads` 的 grep 全部拦截可能让依赖 grep 的合法工作流被误拦，需在真实使用中回归。
