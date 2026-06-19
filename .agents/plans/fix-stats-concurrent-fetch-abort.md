# 功能: fix-stats-concurrent-fetch-abort（修复 ISSUE-005：stats 并发请求被全局 controller 互相 abort）

下面这份计划应尽可能完整，但在真正开始实现前，你仍然必须再次确认 `internal/chatlog/http/static/index.htm` 对应行号的代码没有漂移（用 grep 校验锚点），再动手编辑。

特别注意：**`index.htm` 超过 11000 行，且两处目标函数缩进风格不同（`fetchWithTimeout` 是 tab+空格混合，`switchTab` 是纯空格）。用 Edit 时必须逐字节复制现有缩进，否则匹配失败。**

执行模型循环能力弱：严格逐 Task 执行，每个 Task 只改一处，做完立即跑 VALIDATE，通过再继续。

## 功能描述

修复微信本地数据看板（dashboard）"今日总览"统计只显示 1 个群、其余群数据丢失的缺陷。根因是上一个修复（ISSUE-001，commit 3d10f175）为消除切 tab 时的 long-poll 连接泄漏，在 `fetchWithTimeout` 内部引入了一个**全局单 `AbortController`**，并在"每次发起新请求时 abort 上一个"。但 dashboard overview 是**并发**对所有群调用 `fetchWithTimeout`（`groups.map` + `Promise.all`），于是这些并发请求互相 abort——只有最后一个群能完成，其余全部被取消。本修复把取消机制从"发起新请求即取消"改为"切 tab 时取消在途请求"，用一个 `Set` 追踪所有在途 controller，从而既保留 ISSUE-001 的 tab-switch 取消能力，又让并发请求互不干扰。

## 用户故事

作为一名 chatlog 看板用户
我想要 dashboard"今日总览"准确统计**所有**群聊（消息数、群数、活跃发言人）
以便我看到的是完整的全局数据，而不是只有一个群的残缺数字。

## 问题陈述

ISSUE-005（TODOS.md status=open）：dashboard overview 的群数 / 消息数 / 活跃发言人严重偏小。

根因链（已实测确认）：
1. ISSUE-001（3d10f175）把取消逻辑放进 `fetchWithTimeout`（[index.htm:3376-3390](internal/chatlog/http/static/index.htm:3376)）：每次新调用先 `lastLongPollController.abort()` 再覆盖为新 controller。
2. dashboard overview（[index.htm:3034-3041](internal/chatlog/http/static/index.htm:3034)）用 `const promises = groups.map(async g => fetchWithTimeout('/api/v1/stats?chat='+g.username...))` + `await Promise.all(promises)`——**并发**对每个群发请求。
3. 并发场景下：群2 的 `fetchWithTimeout` 一启动就 abort 群1 的 controller，群3 abort 群2……最终只有最后一个群的请求存活，其余进入 `catch(e){return null}` → `statsList` 几乎只剩 1 个群（[index.htm:3042](internal/chatlog/http/static/index.htm:3042)）。
4. 这是 ISSUE-001 引入的**回归**：在 3d10f175 之前 `fetchWithTimeout` 无全局 abort，并发 stats 都能完成。

ISSUE-001 的设计错误：取消时机绑定到"发起新请求"（错误对象），而应绑定到"切 tab"（真正需要取消在途 long-poll 的时刻）。

## 方案陈述

采用"Set 追踪 + 在 `switchTab` 取消"的架构修复（已与用户确认）：

1. 把单变量 `lastLongPollController` 换成 `const inflightControllers = new Set()`。
2. `fetchWithTimeout` 不再"发起时 abort 上一个"——改为把自己的 controller 加入 Set，`finally` 里从 Set 删除。每个并发请求持有独立 controller，互不干扰。
3. 在 `switchTab(tabId)` 函数体最前面 abort 当前所有在途 controller 并清空 Set——这是 ISSUE-001 真正想要的取消时机（切 tab 时取消旧 tab 仍在跑的 long-poll，消除 ERR_CONNECTION_REFUSED 噪音）。

效果：
- 并发 stats（groups.map）：N 个 controller 各自独立 → N 个请求都完成 → ISSUE-005 修复。
- 切 tab：abort 所有在途（比原来只 abort 最后一个更彻底）→ ISSUE-001 的 tab-switch 噪音仍消除。

## 功能元数据

**功能类型**: 缺陷修复（修正前一修复引入的回归）
**预估复杂度**: 低（3 处集中的前端 JS 改动，无后端、无 schema、无依赖变更）
**主要受影响系统**: 前端单页应用 `internal/chatlog/http/static/index.htm`（go:embed 进 binary）；间接影响所有经 `fetchWithTimeout` 的请求的取消行为
**依赖项**: 无新依赖。构建依赖 cgo（项目既有）。

---

## 上下文参考

### 相关代码文件 — 实现前你必须先阅读/校验这些！

- [internal/chatlog/http/static/index.htm:2008](internal/chatlog/http/static/index.htm:2008) — 当前 `let lastLongPollController = null;`（模块级变量，纯空格 8 缩进）。改 1。
- [internal/chatlog/http/static/index.htm:3376-3391](internal/chatlog/http/static/index.htm:3376) — `fetchWithTimeout` 函数（**tab+空格混合缩进**）。改 2，删除开头全局 abort 块。
- [internal/chatlog/http/static/index.htm:2086-2092](internal/chatlog/http/static/index.htm:2086) — `function switchTab(tabId)`（纯空格缩进，body 12 空格）。改 3，函数体开头插入 abort 全部。
- [internal/chatlog/http/static/index.htm:3034-3042](internal/chatlog/http/static/index.htm:3034) — 受害的并发 stats 调用（**只读，不改**）。这是修复要恢复的功能。
- [internal/chatlog/http/route.go:38-39](internal/chatlog/http/route.go:38) — `//go:embed static` + `var EFS embed.FS`。**原因：index.htm 被 embed 进 binary，改完必须重新 `make build` 才生效**，否则运行中的服务仍是旧前端。
- `TODOS.md`（ISSUE-005 段，status=open）— 修复完成后更新 status。

### fetchWithTimeout 的全部调用点（评估改动影响面，均无需修改）

- 2971 / 2985 dashboard/trend（单独 await，顺序）
- 3036 stats（**并发，受害者**）
- 3072 semantic/index/status（顺序）
- 3419 sessions（顺序）
- 3473 stats（单独 await）
- 改动后语义：顺序调用行为不变；并发调用从"互相取消"变为"各自独立完成"（修复）；切 tab 时全部取消（保留 ISSUE-001 意图）。

### 需要遵循的模式

**缩进（关键，Edit 匹配成败所在）**：
- `fetchWithTimeout`（3376）每行以 `\t`（一个 tab）开头，再接空格对齐。例：函数体行是 `\t` + 12 空格。
- `switchTab`（2086）与 `lastLongPollController`（2008）是纯空格：函数声明 8 空格，函数体 12 空格。
- **编辑时直接复制本计划给出的 old_string（保留原始 tab/空格），不要手敲缩进。**

**abort 错误吞掉**：被取消的请求会抛 `AbortError`，沿用 ISSUE-001 的 `try{...}catch(e){/* swallow */}` 模式，避免 console 噪音。stats 调用点（3039）已有 `catch(e){return null}` 兜底。

**go:embed 构建**：前端改动经 `make build` 重新打包进 `bin/chatlog`；运行态生效需重启服务。

**隐私边界（AGENTS.md）**：dashboard 显示真实群聊统计。验证时**只读取数字计数**（`#dashboard-ov-groups` 的 textContent、`#dashboard-comparison-grid` 子元素个数），**不读取/不打印/不外传群名、消息正文、截图全屏**。断言用"群数 > 1 且等于会话列表中的群总数"，不暴露具体哪些群。

**commit 规范**：沿用项目格式 `fix(qa): ISSUE-005 — <summary>`，结尾加 `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`。本项目 direct-to-main，单会话执行，commit 需用户当轮确认。

---

## 实现计划

### 阶段 1：固定修复前 baseline（只读，证明 bug）
用 agent-browser 记录修复前 dashboard overview 的群数（应远小于实际群数），作为修复后对比基线。

### 阶段 2：代码修复（3 处集中编辑）
变量 → Set；fetchWithTimeout 去全局 abort 改 Set add/delete；switchTab 开头 abort 全部。

### 阶段 3：构建并重启（runtime，需用户授权）
make build 重新 embed；重启 5030 服务。

### 阶段 4：runtime 验证 + 回归 + 收尾
agent-browser 验证所有群显示 + 切 tab 无噪音；go build/harness 回归；更新 TODOS.md。

---

## 分步任务

严格按顺序。每个 Task 原子，做完立即跑 VALIDATE。

### Task 0.1 — VERIFY 服务在跑 + 锚点未漂移（只读）
- **IMPLEMENT**: 确认 5030 在跑、三个目标行号仍是预期代码。
- **VALIDATE**:
  - `curl -sS --max-time 6 http://127.0.0.1:5030/health` 期望 `{"status":"ok"}`
  - `sed -n '2008p;3377p;3381p' internal/chatlog/http/static/index.htm | grep -c lastLongPollController` 期望 `3`（确认锚点未漂移）
  - `sed -n '2086p' internal/chatlog/http/static/index.htm | grep -c "function switchTab"` 期望 `1`

### Task 0.2 — RECORD 修复前 baseline（agent-browser，只读数字）
- **IMPLEMENT**: 用 agent-browser 打开 `http://127.0.0.1:5030/`，默认在 dashboard tab，等 overview 加载完，读取 `#dashboard-ov-groups` 的数字，并数会话列表里 `chat_type==='group'` 的群总数（可从已加载页面状态或 `/api/v1/sessions?format=json` 的 group 计数）。
- **GOTCHA**: 只记录两个数字（overview 群数 vs 实际群总数），**不读群名/消息内容，不外传截图**。预期 bug 表现：overview 群数 ≈ 1，远小于实际群总数。
- **VALIDATE**: 记录 `baseline_overview_groups` 与 `actual_group_total` 两个整数到执行日志；断言 `baseline_overview_groups < actual_group_total`（证明 bug 存在）。若两者已相等，说明 bug 未复现，停下来重新核对根因。

### Task 1.1 — UPDATE 变量声明（index.htm:2008）
- **IMPLEMENT**: 把单 controller 变量换成 Set。
- **old_string**（纯空格 8 缩进）:
  ```
          let lastLongPollController = null;
  ```
- **new_string**:
  ```
          const inflightControllers = new Set();
  ```
- **VALIDATE**: `grep -n "inflightControllers = new Set" internal/chatlog/http/static/index.htm` 期望命中 1 处；`grep -c "lastLongPollController" internal/chatlog/http/static/index.htm` 期望 `4`（2008 已改，剩 3377/3378/3381/3387/3388 中尚未改的——注意此刻仍为旧 fetchWithTimeout，下一步处理）。

### Task 1.2 — UPDATE fetchWithTimeout 去掉全局 abort，改用 Set（index.htm:3376-3391）
- **IMPLEMENT**: 删除开头的全局 abort 块（破坏并发的根源），把 controller 加入/移出 Set。
- **old_string**（注意：每行以一个 tab 开头，本块内 `\t` 表示那个 tab；直接从文件复制以保真）:
  ```
  	        async function fetchWithTimeout(url, timeoutMs = 20000) {
  	            if (lastLongPollController) {
  	                try { lastLongPollController.abort(); } catch (e) { /* swallow */ }
  	            }
  	            const controller = new AbortController();
  	            lastLongPollController = controller;
  	            const timer = setTimeout(() => controller.abort(), timeoutMs);
  	            try {
  	                return await fetch(url, { signal: controller.signal });
  	            } finally {
  	                clearTimeout(timer);
  	                if (lastLongPollController === controller) {
  	                    lastLongPollController = null;
  	                }
  	            }
  	        }
  ```
- **new_string**:
  ```
  	        async function fetchWithTimeout(url, timeoutMs = 20000) {
  	            const controller = new AbortController();
  	            inflightControllers.add(controller);
  	            const timer = setTimeout(() => controller.abort(), timeoutMs);
  	            try {
  	                return await fetch(url, { signal: controller.signal });
  	            } finally {
  	                clearTimeout(timer);
  	                inflightControllers.delete(controller);
  	            }
  	        }
  ```
- **GOTCHA**: 保持每行的 tab+空格缩进与原文件一致。若 Edit 报"old_string 未找到"，先 `sed -n '3376,3391p' internal/chatlog/http/static/index.htm | cat -A` 看真实缩进字符（`^I`=tab）再调整。
- **VALIDATE**: `grep -c "lastLongPollController" internal/chatlog/http/static/index.htm` 期望 `0`（全部清除）；`grep -c "inflightControllers" internal/chatlog/http/static/index.htm` 期望 `3`（声明 + add + delete）。

### Task 1.3 — ADD switchTab 开头取消所有在途请求（index.htm:2086）
- **IMPLEMENT**: 在 `switchTab` 函数体最前面插入 abort 全部 + 清空。
- **old_string**（纯空格缩进）:
  ```
          function switchTab(tabId) {
              document.querySelectorAll('.tab-btn').forEach(btn => btn.classList.remove('active'));
  ```
- **new_string**:
  ```
          function switchTab(tabId) {
              // ISSUE-005 fix: cancel in-flight long-poll fetches on tab switch
              // (correct cancel timing; replaces ISSUE-001 per-call global abort
              //  that broke concurrent stats requests)
              inflightControllers.forEach(c => { try { c.abort(); } catch (e) { /* swallow */ } });
              inflightControllers.clear();
              document.querySelectorAll('.tab-btn').forEach(btn => btn.classList.remove('active'));
  ```
- **GOTCHA**: 纯空格，函数体 12 空格缩进。switchTab 也在 [index.htm:2627](internal/chatlog/http/static/index.htm:2627) 初始化时被调一次，此时 Set 为空，abort 无副作用（安全）。
- **VALIDATE**: `grep -n "ISSUE-005 fix: cancel in-flight" internal/chatlog/http/static/index.htm` 期望命中 1 处；`grep -c "inflightControllers" internal/chatlog/http/static/index.htm` 期望 `5`（声明 + add + delete + forEach + clear）。

### Task 2.1 — VERIFY JS 结构无明显破损（静态，尽力而为）
- **IMPLEMENT**: index.htm 是 HTML 内联 JS，无标准 lint。做轻量结构检查：确认 `fetchWithTimeout`/`switchTab` 函数花括号配对、无残留 `lastLongPollController`。
- **VALIDATE**:
  - `grep -c "lastLongPollController" internal/chatlog/http/static/index.htm` 期望 `0`
  - `sed -n '3376,3387p' internal/chatlog/http/static/index.htm` 人工核对：函数体只剩 `controller`/`inflightControllers`/`timer`/`fetch`，无 `if (lastLongPollController`。

### Task 2.2 — BUILD 重新打包 embed（让前端改动进 binary）
- **IMPLEMENT**: `make build`（index.htm 经 go:embed 打包进 bin/chatlog）。
- **GOTCHA**: 需 cgo（项目默认）。构建失败会输出 Go 编译错误——但本次只改 .htm（不影响 Go 编译），失败通常是环境问题。
- **VALIDATE**: `make build && echo BUILD_OK` 期望 `BUILD_OK`；`ls -la bin/chatlog | awk '{print $6,$7,$8}'` 确认 mtime 更新到当前时间。

### Task 3.1 — RESTART 5030 服务（⚠️ runtime 破坏性，需用户当轮授权）
- **IMPLEMENT**: 停掉旧进程并用新 binary 重启。**执行前必须取得用户同轮授权**（短暂中断服务）。停旧：`kill <pid>`（pid 取自 `pgrep -f "chatlog serve"`）；启新：沿用现有启动方式 `nohup ./bin/chatlog serve --config-dir .cache/daily-report-config >/tmp/chatlog-5030.log 2>&1 &`（与当前运行参数一致）。
- **GOTCHA**: 不要 `git push`/不动数据；只重启进程。若用户不授权重启，跳到 Task 4.x 只做静态验证，runtime 项标 `未验证`。
- **VALIDATE**: `sleep 2 && curl -sS --max-time 6 http://127.0.0.1:5030/health` 期望 `{"status":"ok"}`；`pgrep -f "chatlog serve" | head -1` 返回新 pid。

### Task 3.2 — VALIDATE 修复后所有群显示（agent-browser，核心验收）
- **IMPLEMENT**: 用 agent-browser 打开 `http://127.0.0.1:5030/`，等 dashboard overview 加载，读取 `#dashboard-ov-groups` 数字与 `#dashboard-comparison-grid` 子卡片数量。
- **GOTCHA**: 隐私——只读数字/数元素个数，不读群名/消息、不外传全屏截图。
- **VALIDATE**:
  - `fixed_overview_groups` 数字断言 `== actual_group_total`（或显著 > Task 0.2 的 `baseline_overview_groups`）
  - `#dashboard-comparison-grid` 卡片数 `> 1` 且约等于群总数
  - browser console 无未捕获异常

### Task 3.3 — VALIDATE 切 tab 不回归 ISSUE-001（agent-browser）
- **IMPLEMENT**: 在 dashboard 加载未完时（或加载后）点击切到"全局搜索"再切回"仪表盘 & 数据库"，观察 console。
- **VALIDATE**: 切 tab 过程 console **无** `ERR_CONNECTION_REFUSED`、**无**未吞掉的 `AbortError`（被 abort 的在途请求应被 `catch` 吞掉）。

### Task 4.1 — VERIFY 全仓回归（只读，无 quota）
- **IMPLEMENT**: 确认前端改动没碰坏 Go 侧/harness。
- **VALIDATE**:
  - `go build ./... && echo BUILD_OK` 期望 `BUILD_OK`
  - `node scripts/check-root-harness.mjs | tail -1` 期望 `80/80 passed`

### Task 4.2 — UPDATE TODOS.md（ISSUE-005 收尾）
- **IMPLEMENT**: 把 TODOS.md 中 ISSUE-005 的 `Status: open` 改为 `Status: fixed (3-controller Set + switchTab abort)`，补一行修复 commit 待填。
- **VALIDATE**: `grep -A1 "ISSUE-005" TODOS.md | grep -c "fixed"` 期望 ≥1。

### Task 4.3 — COMMIT（⚠️ 需用户当轮确认）
- **IMPLEMENT**: `git add internal/chatlog/http/static/index.htm TODOS.md` 提交：
  ```
  fix(qa): ISSUE-005 — stats concurrent fetch no longer self-aborts

  ISSUE-001 introduced a single global lastLongPollController that aborted
  the previous in-flight fetch on every new fetchWithTimeout call. The
  dashboard overview fires concurrent /api/v1/stats requests (groups.map +
  Promise.all), so they aborted each other and only the last group survived.

  Replace the single controller with an inflightControllers Set; move the
  cancel point from "new request" to switchTab() so concurrent requests run
  independently while tab switches still cancel in-flight long-polls.

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```
- **GOTCHA**: 不提交 bin/chatlog（构建产物，已 .gitignore `/chatlog-*`？确认 bin/ 被忽略：`.gitignore` 有 `bin/`）。只提交 index.htm + TODOS.md。
- **VALIDATE**: `git show --stat HEAD --name-only | grep -vE "index.htm|TODOS.md" | grep -v "^$" | grep -c .` 期望 `0`（无多余文件）。

---

## 测试策略

前端内联 JS 缺陷，无 Go 单测覆盖。验证以"行为对比 + 构建 + 运行态浏览器"为主。

### 单元测试
- 无新增 Go 单测（改动在 index.htm）。回归用 `go build ./...` 保证 embed/编译不破。

### 集成测试
- baseline 对比（Task 0.2 vs 3.2）：修复前 overview 群数 ≈1 → 修复后 = 群总数。这是本修复的核心断言。

### 边界情况
- **并发**：N 个群（N>1）的 stats 同时发起，全部完成（核心场景）。
- **切 tab 取消**：dashboard 加载中切走，在途请求被 abort 且不报未捕获错误（ISSUE-001 不回归）。
- **初始化调用**：switchTab('chatlog')（2627）在 Set 为空时调用 abort/clear，无副作用。
- **timeout 仍生效**：单请求超过 timeoutMs 仍被自身 setTimeout abort（行为不变）。

---

## 验证命令

### 级别 1：静态/锚点
```bash
grep -c "lastLongPollController" internal/chatlog/http/static/index.htm   # 期望 0
grep -c "inflightControllers" internal/chatlog/http/static/index.htm      # 期望 5
```

### 级别 2：构建/回归
```bash
make build && echo BUILD_OK
go build ./... && echo GO_OK
node scripts/check-root-harness.mjs | tail -1                              # 80/80
```

### 级别 3：运行态（需服务重启后）
```bash
curl -sS --max-time 6 http://127.0.0.1:5030/health                        # {"status":"ok"}
```

### 级别 4：手动验证（agent-browser，隐私：只读数字）
- 打开 `http://127.0.0.1:5030/` → dashboard
- 断言 `#dashboard-ov-groups` == 群总数（修复前 ≈1）
- `#dashboard-comparison-grid` 卡片数 > 1
- 切 tab 来回，console 无 `ERR_CONNECTION_REFUSED` / 未捕获 `AbortError`

---

## 验收标准

- [ ] `index.htm` 无残留 `lastLongPollController`（grep=0）
- [ ] `inflightControllers` 出现 5 处（声明/add/delete/forEach/clear）
- [ ] `make build` 成功，bin/chatlog mtime 更新
- [ ] `go build ./...` 通过、harness 80/80（无回归）
- [ ] 重启后 dashboard overview 群数 = 实际群总数（修复前 ≈1）
- [ ] dashboard-comparison-grid 显示多个群卡片
- [ ] 切 tab 时 console 无 ERR_CONNECTION_REFUSED / 未捕获 AbortError（ISSUE-001 不回归）
- [ ] TODOS.md ISSUE-005 标记 fixed
- [ ] commit 只含 index.htm + TODOS.md
- [ ] 验证全程未打印群名/消息正文/真实数据

## 完成检查清单

- [ ] 3 处编辑各自 VALIDATE 通过
- [ ] 级别 1-3 命令全绿
- [ ] agent-browser 确认所有群显示 + 切 tab 无噪音
- [ ] ISSUE-005 在 TODOS.md 收尾
- [ ] 回滚锚点：修复前 HEAD = `c229bccb`

## 备注

- **为何不选最小 hack（fetchWithTimeout 加 opt-out 参数）**：那保留了"发起新请求即取消上一个"的错误时机，未来任何新的并发 `fetchWithTimeout` 调用都会再次踩坑；Set+switchTab 是把取消时机绑定到正确事件，根治。
- **为何 abort 时机选 switchTab**：ISSUE-001 的真实目标就是"切 tab 时取消旧 tab 的 long-poll"。把 abort 放在 switchTab 是语义最贴合的位置，且天然不影响同一 tab 内的并发请求。
- **运行 binary 必须重建**：index.htm 是 go:embed（route.go:38），不 `make build` 则改动不进运行中的服务——这是本项目反复踩的坑（stale binary）。Task 2.2/3.1 专门处理。
- **隐私**：dashboard 是真实聊天统计。本计划所有验证只读数字计数、不读内容、不外传截图，符合 AGENTS.md 边界。
- **不走 Ralph**：本任务含 commit 且涉及既有文件运行态验证，与 Ralph 的 developer-不-commit + 排除-dirty-文件 模型冲突（见 .agents/plans/tidy-worktree-and-commit.md 同款分析），故单会话执行。

---

**信心分数：8/10**（一次执行成功的把握）
- 加分：根因经代码级实测确认（并发 groups.map + 全局 controller）；修复方案语义正确且改动集中（3 处）；每处给出逐字节 before/after + 缩进警告 + grep 断言。
- 扣分：MiniMax 在 11000+ 行大文件、两种缩进风格下做精确 Edit 有匹配失败风险（缓解：Task 给了 `cat -A` 排查指引）；核心验收依赖重启服务（需授权）+ agent-browser 运行态（依赖外部状态可用）；纯前端改动无自动化单测，最终正确性靠浏览器行为对比。
