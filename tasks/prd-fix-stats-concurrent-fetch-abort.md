# PRD: 修复 ISSUE-005 — dashboard stats 并发请求被全局 controller 互相 abort

## Introduction / 概述

微信本地数据看板（dashboard）的"今日总览"统计（群数 / 消息数 / 活跃发言人）只显示约 1 个群的数据，其余群丢失。

根因（已代码级实测确认）：上一个修复 ISSUE-001（commit `3d10f175`）为消除"切 tab 时 long-poll 连接泄漏"，在 `fetchWithTimeout` 内引入一个**全局单 `AbortController`**（`lastLongPollController`），并在"每次发起新请求时 abort 上一个"。但 dashboard overview 是**并发**对所有群调用 `fetchWithTimeout`（`groups.map` + `Promise.all`，[index.htm:3034-3041](internal/chatlog/http/static/index.htm:3034)），于是这些并发请求互相 abort——只有最后一个群存活，其余进入 `catch{return null}`。**ISSUE-005 是 ISSUE-001 引入的回归。**

本 PRD 把取消时机从"发起新请求即取消"改为"切 tab 时取消在途请求"，用 `Set` 追踪所有在途 controller：并发请求各自独立完成（修 ISSUE-005），切 tab 仍取消在途 long-poll（保留 ISSUE-001 意图）。

完整实现计划见 `.agents/plans/fix-stats-concurrent-fetch-abort.md`。

## Goals

- dashboard overview 统计涵盖**所有**群（群数 = 实际群总数，而非 ≈1）
- 保留 ISSUE-001 的能力：切 tab 时取消旧 tab 在途 long-poll，无 `ERR_CONNECTION_REFUSED` 噪音
- 取消机制改为语义正确的设计（绑定到 `switchTab`，而非每次新请求）
- 零后端改动、零依赖变更、零回归（go build / harness 不破）

## User Stories

> 执行方式：**单会话执行**（不走 Ralph，不重建工作分支）。核心修复的 3 处改动**强耦合**，合并为单个原子 story（US-002），避免中间半改状态破坏 JS。

### US-001: 记录修复前 baseline（证明 bug 存在）
**描述：** 作为修复者，我需要先固定修复前的现象，以便修复后能客观对比。

**Acceptance Criteria：**
- [ ] `curl -sS --max-time 6 http://127.0.0.1:5030/health` 返回 `{"status":"ok"}`
- [ ] 使用 agent-browser 打开 `http://127.0.0.1:5030/`，停在默认的"仪表盘 & 数据库" tab，等待 overview 加载完成
- [ ] 读取 `#dashboard-ov-groups` 的数字，记为 `baseline_overview_groups`（整数）
- [ ] 通过 `/api/v1/sessions?format=json` 统计 `chat_type==='group'` 的群总数，记为 `actual_group_total`（整数）
- [ ] 断言 `baseline_overview_groups < actual_group_total`（证明 bug：总览群数远小于实际群数）
- [ ] 隐私：仅记录两个整数，**不读取/不打印群名、消息正文，不外传全屏截图**
- [ ] 若 `baseline_overview_groups == actual_group_total`（bug 未复现），停止并重新核对根因，不继续修复

### US-002: 核心修复 — 全局单 controller 改为 Set + 取消时机移到 switchTab（3 处原子改动）
**描述：** 作为前端开发者，我需要在同一次编辑里改完 3 处耦合代码，使并发请求互不取消、切 tab 仍取消在途请求。

**Acceptance Criteria：**
- [ ] 改 1（[index.htm:2008](internal/chatlog/http/static/index.htm:2008)）：`let lastLongPollController = null;` → `const inflightControllers = new Set();`
- [ ] 改 2（[index.htm:3376-3391](internal/chatlog/http/static/index.htm:3376) `fetchWithTimeout`）：删除函数开头的 `if (lastLongPollController){...abort...}` 块；`const controller = new AbortController();` 后改为 `inflightControllers.add(controller);`；`finally` 块用 `inflightControllers.delete(controller);` 替代原 `if (lastLongPollController === controller) lastLongPollController = null;`
- [ ] 改 3（[index.htm:2086](internal/chatlog/http/static/index.htm:2086) `switchTab` 函数体首行）：插入 `inflightControllers.forEach(c => { try { c.abort(); } catch (e) {} }); inflightControllers.clear();`
- [ ] 三处改动在同一次编辑内完成，**期间不提交**（避免半改状态）
- [ ] `grep -c "lastLongPollController" internal/chatlog/http/static/index.htm` 输出 `0`
- [ ] `grep -c "inflightControllers" internal/chatlog/http/static/index.htm` 输出 `5`（声明 + add + delete + forEach + clear）
- [ ] 缩进保真：`fetchWithTimeout` 为 tab+空格混合，`switchTab`/变量为纯空格；编辑后 `sed -n '3376,3387p' internal/chatlog/http/static/index.htm | cat -A` 人工确认无缩进破损

### US-003: 重新构建以打包前端改动（go:embed）
**描述：** 作为维护者，我需要重新构建 binary，因为 index.htm 是 `go:embed`（[route.go:38](internal/chatlog/http/route.go:38)），不重建则改动不生效。

**Acceptance Criteria：**
- [ ] `make build` 成功（exit 0）
- [ ] `bin/chatlog` 的 mtime 更新为当前构建时间（`ls -la bin/chatlog`）
- [ ] `go build ./...` 通过（exit 0，确认前端改动未连带破坏 Go 编译）

### US-004: 重启 5030 服务加载新 binary（runtime，需用户授权）
**描述：** 作为维护者，我需要用新 binary 重启服务，使修复在运行态生效。

**Acceptance Criteria：**
- [ ] **执行前取得用户当轮授权**（重启会短暂中断服务）
- [ ] 停止旧进程：`kill $(pgrep -f "chatlog serve")`
- [ ] 用与原参数一致的方式启动新进程：`nohup ./bin/chatlog serve --config-dir .cache/daily-report-config >/tmp/chatlog-5030.log 2>&1 &`
- [ ] `sleep 2 && curl -sS --max-time 6 http://127.0.0.1:5030/health` 返回 `{"status":"ok"}`
- [ ] `pgrep -f "chatlog serve"` 返回新的 pid（与重启前不同）
- [ ] 若用户不授权：本 story 标 `未验证`，US-005/US-006 runtime 验证一并标 `未验证`，仅保留静态验证

### US-005: 闭环验证 — dashboard 显示所有群（前端→后端→UI）
**描述：** 作为看板用户，我需要确认 overview 统计涵盖所有群，证明并发 stats 不再自相 abort。

**Acceptance Criteria：**
- [ ] 使用 agent-browser 打开 `http://127.0.0.1:5030/`，停在"仪表盘 & 数据库" tab，等待 overview 加载完成
- [ ] 并发的 `/api/v1/stats?format=json&chat=...` 请求真实到达后端并返回 HTTP 200（不是 404/被 abort 的失败），可经浏览器 network 面板确认多个 stats 请求均 200
- [ ] 读取 `#dashboard-ov-groups` 数字，记为 `fixed_overview_groups`
- [ ] 断言 `fixed_overview_groups == actual_group_total`（或显著大于 US-001 的 `baseline_overview_groups`）
- [ ] `#dashboard-comparison-grid` 的子卡片数量 `> 1` 且约等于群总数
- [ ] 浏览器 console 无未捕获异常
- [ ] 隐私：仅读取数字与元素计数，**不读群名/消息正文，不外传全屏截图**

### US-006: 回归验证 — 切 tab 不回归 ISSUE-001
**描述：** 作为维护者，我需要确认切 tab 仍能取消在途 long-poll，没有把 ISSUE-001 的连接泄漏改回来。

**Acceptance Criteria：**
- [ ] 使用 agent-browser 打开 `http://127.0.0.1:5030/` 的 dashboard，在 overview 加载期间或加载后点击切到"全局搜索" tab，再切回"仪表盘 & 数据库"
- [ ] 切 tab 过程浏览器 console **无** `ERR_CONNECTION_REFUSED`
- [ ] 切 tab 过程浏览器 console **无**未捕获的 `AbortError`（被取消的在途请求应被 `catch` 吞掉）
- [ ] 页面切回后 overview 能正常重新加载并再次显示所有群

### US-007: 仓库回归 + ISSUE-005 收尾 + 提交
**描述：** 作为维护者，我需要确认全仓无回归并把修复落账。

**Acceptance Criteria：**
- [ ] `go build ./...` 通过（exit 0）
- [ ] `node scripts/check-root-harness.mjs` 输出 `80/80 passed`
- [ ] `TODOS.md` 中 ISSUE-005 的 `Status: open` 改为 `Status: fixed`
- [ ] **取得用户当轮授权后**提交：`git add internal/chatlog/http/static/index.htm TODOS.md` + commit message 以 `fix(qa): ISSUE-005 — ` 开头，结尾含 `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`
- [ ] `git show --stat HEAD --name-only` 仅含 `index.htm` 与 `TODOS.md`（无 bin/chatlog 等多余文件；`bin/` 已被 .gitignore）

## Functional Requirements

- FR-1: 用模块级 `const inflightControllers = new Set()` 替代单一 `lastLongPollController`
- FR-2: `fetchWithTimeout` 发起请求时**不得** abort 其他在途 controller；只将自身 controller `add` 进 Set，并在 `finally` 中 `delete`
- FR-3: `switchTab(tabId)` 被调用时，必须 abort 当前 Set 内所有在途 controller 并 `clear()`
- FR-4: dashboard overview 的并发 `/api/v1/stats` 请求必须全部完成，`statsList` 涵盖所有有数据的群
- FR-5: 单个请求超过 `timeoutMs` 仍由其自身 `setTimeout(() => controller.abort())` 取消（超时行为不变）
- FR-6: 被取消的请求抛出的 `AbortError` 必须被吞掉，不产生 console 噪音

## Non-Goals（超出范围）

- 不改后端 `/api/v1/stats` 接口逻辑
- 不重构其他 `fetchWithTimeout` 调用点（2971/2985/3072/3419/3473 顺序调用，行为不变）
- 不把 `runLimited`(3535) 的 worker（当前用别的 fetch）纳入本次改动
- 不引入 per-endpoint controller 的复杂方案
- 不处理 ISSUE-005 以外的其他 QA issue
- 不修改 commit author / 不 force-push 历史 commit

## Design Considerations

- 复用 ISSUE-001 既有的 `try{...}catch(e){/* swallow */}` 吞 abort 错误模式
- `switchTab` 在 [index.htm:2627](internal/chatlog/http/static/index.htm:2627) 初始化时被调一次，此时 Set 为空，abort/clear 无副作用（安全）
- 验证 UI 时只读数字计数，符合 AGENTS.md 隐私边界

## Technical Considerations

- **go:embed 重建**：index.htm 改完必须 `make build` 才进 binary（本项目反复踩的 stale-binary 坑）；runtime 生效需重启服务
- **缩进陷阱**：`fetchWithTimeout`(3376) 是 tab+空格混合，`switchTab`(2086)/变量(2008) 是纯空格；编辑需逐字节保真
- **隐私**：dashboard 是真实聊天统计；所有 agent-browser 验证只读数字/元素计数，不读内容、不外传截图
- **执行模型**：单会话执行（人工/Claude），不走 Ralph，不重建工作分支；修复前 HEAD = `c229bccb`（回滚锚点）
- **无自动化单测**：纯前端内联 JS，正确性靠 baseline（US-001）vs fixed（US-005）行为对比

## Success Metrics

- dashboard overview 群数从修复前 ≈1 恢复到 = 实际群总数
- 切 tab 时 console `ERR_CONNECTION_REFUSED` 数量为 0
- `go build ./...` 与 harness 80/80 无回归

## Open Questions

- `runLimited`(3535) 的 DB probe worker 当前未用 `fetchWithTimeout`；若未来改用，是否也需纳入 `inflightControllers` Set 管理？（本次不处理）
- 是否需要为 dashboard 之外其他并发 fetch 场景统一加超时/取消策略？（超出本次范围）
