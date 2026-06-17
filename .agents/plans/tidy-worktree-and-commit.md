# 功能: tidy-worktree-and-commit（整理脏工作树并分组提交）

下面这份计划应尽可能完整，但在真正开始执行前，你仍然必须再次验证 `git status`、`.gitignore` 现状以及每条 VALIDATE 命令是否仍然成立（工作树可能已被其他会话改动）。

特别注意：**执行模型循环能力弱。严格逐 Task 执行，每个 Task 只做一件事，做完立刻跑它的 VALIDATE，通过后再进下一个 Task。绝不合并多个 Task。**

## 功能描述

当前 `chatlog_alpha` 仓库工作树长期处于"脏"状态：8 个 modified tracked 文件 + 约 50 个 untracked 文件/目录混在一起，其中夹杂缓存垃圾、指向外部项目的私密聊天数据 symlink、真实产品代码、agent 自动化基础设施、任务文档，以及与代码现状不符的状态文件。本功能把工作树系统化整理干净：垃圾/私密项加 `.gitignore`、AI 博主人设 prompt 删除、有意义的文件分组 atomic 提交、状态三件套修正为 main 真实现状，最终让 `git status` 清爽且 `init.sh`/harness 全绿。

## 用户故事

作为一名 chatlog_alpha 维护者（电信运维工程师）
我想要把脏乱的 git 工作树整理成清爽、可提交、无隐私泄漏风险的状态
以便后续每次 `git status` 都只反映真实待办，不再被缓存垃圾、私密 symlink 和过期状态文件干扰，也不会误提交聊天数据。

## 问题陈述

`git status` 当前混杂以下互不相干的东西，无法直接判断"哪些该提交、哪些该忽略、哪些该删"：

1. **缓存垃圾**：`scripts/ralph/__pycache__/*.pyc`（`.gitignore` 缺 `__pycache__/`）。
2. **私密/外部数据**：`reports` 是 symlink → `/Volumes/WorkSSD/Dev/openclaw_mz/knowledge/raw/微信每日聊天记录`（外部项目的私密聊天目录）；`reports.backup-20260606_124656/`（32 个文件 4MB 日报衍生）。`.gitignore` 里的 `reports/`（带斜杠）匹配不到 symlink，`reports.backup-*` 也无规则 → **有被误提交的真实风险**。
3. **真实产品代码未入库**：`cmd/chatlog/cmd_serve.go`（hidden `serve` 子命令）。
4. **agent 基础设施一直 untracked**：`.agents/`、`.claude/`、`scripts/ralph/*`、`scripts/chatlog-ha-guard.sh`。
5. **状态文件失真**：`feature_list.json` 的 `active_feature_id` 仍指向 `db-runtime-graph-truth-harness-2026-06-12`（status=in_progress），但该 feature 的代码（US-002/003/007/008）已随工作分支删除被丢弃，与 main 现状不符。
6. **不该内置的文件**：`openai_prmpt.md`（AI 博主人设 prompt；用户为运维工程师，明确不内置该人设）。

## 方案陈述

按"先止血、再入库、后修状态"的顺序分 8 个阶段整理：

- **止血**：先用 `.gitignore` 屏蔽垃圾与私密项（不改动其内容），并删除 `openai_prmpt.md`，消除误提交风险。
- **入库**：把有意义文件按内聚边界分组 atomic 提交（产品代码 → agent 基础设施 → 文档/归档），每次提交后立刻验证 HEAD 可编译 + harness 不掉绿。
- **修状态**：把状态三件套修正为反映 main 真实现状（新增 `workspace-tidy` meta feature 作 active，db-runtime 标 `discarded`），最后提交。

整个过程在 `main` 分支进行（与项目现状一致：当前 main 已领先 origin/main 9 个未 push 的 commit）。全程不 `push`、不开新分支、不 `reset`/`rebase`。

## 功能元数据

**功能类型**: 重构（仓库卫生 / 工作树整理，不改产品行为）
**预估复杂度**: 中（机械操作为主，但含私密数据规避、编译安全分组、状态文件内容修正三个易错点）
**主要受影响系统**: git 工作树、`.gitignore`、状态三件套（`feature_list.json`/`progress.md`/`session-handoff.md`）；产品代码与 agent 基础设施仅从 untracked → tracked，内容不变
**依赖项**: 无新外部库；用 git、go、node、python3 现有工具链

---

## 上下文参考

### 相关代码文件 — 执行前你必须先阅读/确认这些！

- `.gitignore` — 原因：已忽略 `.cache/ logs/ outputs/ reports/`（注意 `reports/` 带斜杠只匹配目录，匹配不到 `reports` symlink），缺 `__pycache__/`、`reports.backup-*/`、`/reports`。本计划在文件末尾追加规则。
- `cmd/chatlog/cmd_serve.go` (1-26) — 原因：待入库的产品代码。已确认自洽：`serveConfigDir` 自身定义(L9)、`rootCmd` 在已 tracked 的 `cmd/chatlog/root.go`、`m.CommandHTTPServer` 在 HEAD 的 `internal/chatlog/manager.go:830` 已存在 → 可独立提交，提交后 HEAD 仍编译。
- `internal/chatlog/manager.go` (~853-862) — 原因：modified，把 `log.Info().Msgf("server config: %+v", m.sc)`（会 dump 含 data dir 路径的完整配置）改为结构化脱敏日志（`has_data_dir`/`has_work_dir` 布尔）。隐私改进，独立提交。
- `internal/chatlog/dailyreport/renderer.go` (16,83,106) — 原因：modified，日报标题"今日"→"当日"。
- `skills/chatlog-http-cli/scripts/render-enhanced-daily-html.go` (412) — 原因：modified，同样"今日"→"当日"。与 renderer.go 同组提交。
- `scripts/check-root-harness.mjs` (126-188) — 原因：modified（+58 行新检查项）。`checkFeatureList`(126-) 要求 `active_feature_id` 为非空 string 且**必须匹配某个 feature.id**，每个 feature 含 `id/name/title/description/status/scope/done_criteria/evidence/next_step/dependencies` 10 字段；`checkRalphPrd`(165-) 要求 `branchName` 以 `ralph/` 开头、`userStories` 非空。修正状态文件后必须保持 80/80。
- `feature_list.json` — 原因：modified，`active_feature_id=db-runtime-graph-truth-harness-2026-06-12`，16 个 features，最后一个 status=in_progress/evidence=7 即失真项。
- `AGENTS.md` — 原因：modified（+142 行 create-rules 增量更新）。规则文件，与 check-root-harness.mjs 同组提交。

### 待创建/新增的文件

- `.agents/plans/tidy-worktree-and-commit.md` — 本计划文件（已创建）。
- 修正后的 `feature_list.json` 中新增一个 `workspace-tidy-2026-06-17` meta feature（详见 Phase 7）。

### 需遵循的模式

**Commit message 规范**（项目惯例，见 `git log`：`feat:`/`fix:`/`chore:`/`docs:` 前缀）：
- 每条 commit 结尾加一行（Bash 工具/项目要求）：
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```

**隐私边界（AGENTS.md 强约束，本计划最高优先级）**：
- 绝不 `cat`/打印 `reports`（symlink 指向的聊天目录）、`reports.backup-*/` 任何文件内容。只用 `ls`/`du`/`git check-ignore`/`wc` 验证 path/count/size/忽略状态。
- 绝不打印真实 API key、真实 data key、聊天正文。
- `manager.go` 脱敏改动验证只看 diff 行，不运行会打印真实配置的命令。

**删除安全（AGENTS.md：不要脚本批量删除；一次删一个并说明风险/回滚）**：
- `openai_prmpt.md` 删除是逐文件 `rm`。**风险：该文件从未被 git 跟踪（never-tracked），删除后 git 无法恢复**；如需保险，先 `cp openai_prmpt.md /tmp/openai_prmpt.md.bak` 留本地备份。

**编译安全分组**：每个 commit 后 HEAD 必须能 `go build ./...`。已逐项确认各产品代码改动互不耦合，可任意顺序独立提交。

**git add 精确性**：用显式路径 `git add <path>`，绝不用 `git add -A`/`git add .`（会把私密/垃圾项一起暂存）。每次 commit 前用 `git status --porcelain` 确认暂存区只含本组目标文件。

---

## 实现计划

### 阶段 1：止血（.gitignore + 删人设文件）
屏蔽垃圾与私密项、删除 openai_prmpt.md，先消除误提交风险，再做任何 commit。

### 阶段 2：提交 .gitignore
单独提交忽略规则，使后续 `git status` 干净。

### 阶段 3：分组提交产品代码
manager.go / renderer 组 / cmd_serve.go 各自 atomic 提交，每次提交后 go build。

### 阶段 4：提交 agent/harness 基础设施
scripts/ralph、.agents、.claude、ha-guard、harness+AGENTS.md。

### 阶段 5：提交文档/归档
tasks/、archive/、TODOS.md。

### 阶段 6：修正并提交状态三件套
feature_list.json / progress.md / session-handoff.md 反映 main 现状。

### 阶段 7：最终全量验证 + 回滚锚点记录

---

## 分步任务

重要：严格按顺序从上到下执行。每个 Task 原子且可独立验证；做完立即跑 VALIDATE，通过再继续。

### Task 0.1 — RECORD 回滚锚点（只读）
- **IMPLEMENT**: 记录起始状态，便于回滚。运行并把输出贴到执行日志：`git rev-parse HEAD` + `git status --porcelain=v1 -uall | wc -l`。
- **GOTCHA**: 不做任何修改。当前应在 `main`，HEAD 应为 `2f891fd8`（若不同，停下来核对）。
- **VALIDATE**: `git rev-parse --abbrev-ref HEAD`  期望输出 `main`

### Task 1.1 — UPDATE `.gitignore`（忽略 Python 缓存）
- **IMPLEMENT**: 在 `.gitignore` 末尾追加一行 `__pycache__/` 和一行 `*.pyc`。
- **PATTERN**: 沿用 `.gitignore` 现有分组注释风格（如 `# local build/cache dirs`），新增 `# Python cache` 注释段。
- **GOTCHA**: 只追加，不改动已有规则。
- **VALIDATE**: `git check-ignore scripts/ralph/__pycache__/ralph.cpython-314.pyc`  期望输出该路径（命中=已忽略）

### Task 1.2 — UPDATE `.gitignore`（忽略 reports symlink）
- **IMPLEMENT**: 在 `.gitignore` 末尾追加 `/reports`（不带斜杠，匹配仓库根的 `reports` symlink；现有 `reports/` 带斜杠只匹配目录，匹配不到 symlink）。
- **GOTCHA**: 保留已有的 `reports/` 行不动；新增 `/reports`。两者并存。
- **VALIDATE**: `git check-ignore reports`  期望输出 `reports`（命中）

### Task 1.3 — UPDATE `.gitignore`（忽略 report 备份目录）
- **IMPLEMENT**: 在 `.gitignore` 末尾追加 `reports.backup-*/`。
- **GOTCHA**: 这是私密日报衍生（32 文件 4MB），绝不入库。
- **VALIDATE**: `git check-ignore reports.backup-20260606_124656/`  期望输出该路径（命中）

### Task 1.4 — VERIFY 私密/垃圾项已从 status 消失（只读）
- **IMPLEMENT**: 确认三类项不再出现在 `git status`。
- **VALIDATE**: `git status --porcelain=v1 -uall | grep -E 'reports|__pycache__' || echo CLEAN`  期望输出 `CLEAN`

### Task 2.1 — BACKUP openai_prmpt.md（删除前留本地备份）
- **IMPLEMENT**: `cp openai_prmpt.md /tmp/openai_prmpt.md.bak`（该文件 never-tracked，git 无法恢复，故先备份到 /tmp）。可选：`head -5 openai_prmpt.md` 确认确是 AI 博主人设 prompt（非误删的重要文件）。
- **GOTCHA**: 不要把该文件内容写进任何会被提交的位置。
- **VALIDATE**: `ls -la /tmp/openai_prmpt.md.bak`  期望文件存在

### Task 2.2 — REMOVE openai_prmpt.md（用户已授权删除）
- **IMPLEMENT**: `rm openai_prmpt.md`（单文件删除，非批量）。
- **GOTCHA**: 删除不可逆（git 无历史）；已在 Task 2.1 备份到 /tmp。
- **VALIDATE**: `test ! -e openai_prmpt.md && echo DELETED`  期望输出 `DELETED`

### Task 3.1 — COMMIT .gitignore
- **IMPLEMENT**: `git add .gitignore` 然后提交，message：
  ```
  chore: ignore python cache, reports symlink, and report backups

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```
- **GOTCHA**: 提交前 `git status --porcelain` 确认暂存区**只有** `.gitignore`。openai_prmpt.md 是 never-tracked，删除它不进入本 commit（也无需 commit）。
- **VALIDATE**: `git show --stat HEAD | grep -c gitignore`  期望 ≥1，且 `git show --stat HEAD --name-only | grep -vc gitignore` 应确认无其他文件

### Task 4.1 — COMMIT manager.go（脱敏日志）
- **IMPLEMENT**: `git add internal/chatlog/manager.go` 提交，message：
  ```
  refactor(http): redact server config in startup log

  Replace full %+v config dump (exposed data dir path) with structured
  log carrying only platform/version/addr and has_data_dir/has_work_dir.

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```
- **PATTERN**: 现有 zerolog 结构化日志风格（`log.Info().Str(...).Bool(...).Msg(...)`）。
- **VALIDATE**: `go build ./...`  期望 exit 0

### Task 4.2 — COMMIT 日报"今日→当日"用语（renderer + enhanced html）
- **IMPLEMENT**: `git add internal/chatlog/dailyreport/renderer.go skills/chatlog-http-cli/scripts/render-enhanced-daily-html.go` 提交，message：
  ```
  fix(daily-report): unify wording 今日 -> 当日

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```
- **GOTCHA**: 两文件同一语义改动，合一个 commit 合理。
- **VALIDATE**: `go test ./internal/chatlog/dailyreport`  期望 `ok`

### Task 4.3 — COMMIT cmd_serve.go（serve 子命令）
- **IMPLEMENT**: `git add cmd/chatlog/cmd_serve.go` 提交，message：
  ```
  feat(cli): add hidden serve subcommand

  serve --config-dir starts HTTP server from a service config dir;
  used for headless/runtime startup.

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```
- **PATTERN**: cobra 自注册（`init()` + `rootCmd.AddCommand`），与 `cmd/chatlog/root.go` 一致。
- **VALIDATE**: `go build ./... && go run . serve --help 2>&1 | head -3`  期望编译通过且显示 serve 帮助

### Task 5.1 — COMMIT scripts/ralph 自动化体系
- **IMPLEMENT**: 显式列出要 add 的文件（避开已被忽略的 `__pycache__`）：`git add scripts/ralph/ralph.py scripts/ralph/dashboard.py scripts/ralph/CLAUDE.md scripts/ralph/VALIDATOR.md scripts/ralph/dashboard.html scripts/ralph/dashboard-p.html scripts/ralph/test_branch_merge.sh scripts/ralph/prd.json scripts/ralph/progress.txt` 提交，message：
  ```
  chore(ralph): add PRD-driven automation harness

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```
- **GOTCHA**: 提交后 `git status` 不应再出现 `scripts/ralph/` 下除被忽略 `__pycache__` 外的文件。`prd.json` 当前是 bootstrap placeholder（已是预期状态）。
- **VALIDATE**: `python3 -m py_compile scripts/ralph/ralph.py scripts/ralph/dashboard.py && echo OK`  期望 `OK`；并 `node scripts/check-root-harness.mjs | tail -1` 期望 `80/80 passed`

### Task 5.2 — COMMIT .agents + .claude 本地 skills/commands
- **IMPLEMENT**: `git add .agents .claude` 提交（`.claude/settings.local.json` 已被 .gitignore，不会进暂存）。message：
  ```
  chore(agents): add project-local Codex/Claude skills and commands

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```
- **GOTCHA**: 提交前 `git status --porcelain --cached | grep settings.local.json || echo SAFE` 必须输出 `SAFE`（确认未误纳入本地设置）。本计划文件 `.agents/plans/tidy-worktree-and-commit.md` 会一并进入此 commit，合理。
- **VALIDATE**: `git status --porcelain -uall | grep -E '^\?\? \.(agents|claude)/' || echo CLEAN`  期望 `CLEAN`

### Task 5.3 — COMMIT chatlog-ha-guard.sh
- **IMPLEMENT**: `git add scripts/chatlog-ha-guard.sh` 提交，message：
  ```
  chore(scripts): add chatlog HA guard script

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```
- **VALIDATE**: `bash -n scripts/chatlog-ha-guard.sh && echo SYNTAX_OK`  期望 `SYNTAX_OK`

### Task 5.4 — COMMIT harness 检查扩展 + AGENTS.md
- **IMPLEMENT**: `git add scripts/check-root-harness.mjs AGENTS.md` 提交，message：
  ```
  chore(harness): extend root harness checks and refresh AGENTS.md

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```
- **GOTCHA**: 这俩是 harness 自身 + 规则，逻辑内聚。
- **VALIDATE**: `node scripts/check-root-harness.mjs | tail -1`  期望 `80/80 passed`

### Task 6.1 — COMMIT tasks/ + archive/ + TODOS.md
- **IMPLEMENT**: `git add tasks archive TODOS.md` 提交，message：
  ```
  docs: add PRD sources, task archives, and TODOS

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```
- **GOTCHA**: `archive/` 含历史 PRD 快照（含 2026-06-17 discarded 归档），`tasks/` 含 6 个 PRD 源，`TODOS.md` 是 graph-store-WAL 待办。均为文档，无隐私聊天内容（但执行前可 `grep -rl` 抽查不含真实 key/聊天正文）。
- **VALIDATE**: `git status --porcelain -uall | grep -E '^\?\? (tasks|archive|TODOS)' || echo CLEAN`  期望 `CLEAN`

### Task 7.1 — READ harness 约束 + 状态文件现状（执行内容修正前必读）
- **IMPLEMENT**: 重读 `scripts/check-root-harness.mjs` 的 `checkFeatureList`（126-164 行附近）确认字段要求；读 `feature_list.json` 最后一个 feature（db-runtime）与顶层 `active_feature_id`。
- **GOTCHA**: `active_feature_id` 必须匹配一个真实 feature.id，**不能**设为 `"none"`/空。
- **VALIDATE**: `python3 -c "import json;d=json.load(open('feature_list.json'));print(d['active_feature_id'], len(d['features']))"`  期望打印当前 active id 与 feature 数

### Task 7.2 — UPDATE feature_list.json（新增 workspace-tidy meta feature + db-runtime 标 discarded）
- **IMPLEMENT**:
  1. 把现有 `db-runtime-graph-truth-harness-2026-06-12` feature 的 `status` 改为 `discarded`，`next_step` 改为 `"工作分支已删除、代码(US-002/003/007/008)已丢弃；DB 未真正恢复；如需恢复见 archive/2026-06-17-db-runtime-graph-truth-harness-discarded/"`，evidence 保留作历史。
  2. 在 `features` 数组追加一个新 feature（含全部 10 字段）：
     ```json
     {
       "id": "workspace-tidy-2026-06-17",
       "name": "workspace-tidy",
       "title": "工作树整理与分组提交",
       "description": "把长期脏的工作树整理入库：gitignore 垃圾/私密项、删除 AI 人设 prompt、分组提交产品代码与 agent 基础设施、修正状态三件套为 main 现状。",
       "status": "in_progress",
       "scope": "git 工作树 / .gitignore / 状态三件套；产品代码与基础设施仅 untracked->tracked 不改行为",
       "done_criteria": ["git status 只剩被忽略项", "go build ./... 与核心 go test 通过", "node scripts/check-root-harness.mjs 80/80", "私密 reports symlink 与 reports.backup-* 未入库"],
       "evidence": [],
       "next_step": "执行 .agents/plans/tidy-worktree-and-commit.md Phase 1-7",
       "dependencies": []
     }
     ```
  3. 把顶层 `active_feature_id` 改为 `"workspace-tidy-2026-06-17"`。
- **GOTCHA**: 保持 JSON 合法；用 2 空格缩进与文件现有风格一致。
- **VALIDATE**: `python3 -c "import json;d=json.load(open('feature_list.json'));a=d['active_feature_id'];assert any(f['id']==a for f in d['features']);print('active matches:',a)"`  期望打印 `active matches: workspace-tidy-2026-06-17`

### Task 7.3 — UPDATE progress.md（追加 worktree-tidy 段）
- **IMPLEMENT**: 在 `progress.md` 顶部（或合适的当前状态段）追加本次整理的 Current State / What changed / Verification Evidence / Not Verified（DB 未恢复）/ Next。保留 `Verification Evidence` 与 `Recommended Next Step` 标记（harness checkIncludes 依赖这两个字符串）。
- **GOTCHA**: 不要删除 harness 依赖的小节标题：`Verification Evidence`、`Recommended Next Step`。
- **VALIDATE**: `grep -c -E 'Verification Evidence|Recommended Next Step' progress.md`  期望 ≥2

### Task 7.4 — UPDATE session-handoff.md（刷新 Next Session）
- **IMPLEMENT**: 更新 `session-handoff.md`，写清下一会话起点：工作树已整理、main 领先 origin/main N 个 commit 未 push、db-runtime 已 discarded、DB 仍未恢复（US-004 待授权）。保留 harness 依赖小节：`Next Session`、`Blockers`、`Files`。
- **VALIDATE**: `grep -c -E 'Next Session|Blockers|Files' session-handoff.md`  期望 ≥3

### Task 7.5 — COMMIT 状态三件套
- **IMPLEMENT**: `git add feature_list.json progress.md session-handoff.md` 提交，message：
  ```
  chore(state): record worktree tidy; mark db-runtime feature discarded

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  ```
- **VALIDATE**: `node scripts/check-root-harness.mjs | tail -1`  期望 `80/80 passed`

### Task 8.1 — VERIFY 工作树干净（只读）
- **IMPLEMENT**: 确认 `git status` 只剩被忽略项（被忽略项默认不显示，故 porcelain 应为空）。
- **VALIDATE**: `git status --porcelain=v1 | wc -l | tr -d ' '`  期望 `0`

### Task 8.2 — VERIFY 全量门禁（只读 / 无 quota）
- **IMPLEMENT**: 跑最小诚实门禁。
- **VALIDATE**:
  - `go build ./... && echo BUILD_OK`  期望 `BUILD_OK`
  - `go test ./internal/chatlog/dailyreport ./internal/chatlog/semantic ./internal/chatlog/temporalgraph`  期望全 `ok`
  - `node scripts/check-root-harness.mjs | tail -1`  期望 `80/80 passed`
  - `node skills/chatlog-http-cli/scripts/check-harness-skill.mjs | tail -1`  期望通过

### Task 8.3 — RECORD 最终锚点 + 回填 evidence（只读 + 小改）
- **IMPLEMENT**: `git log --oneline -12` 记录本次所有 commit；把关键 commit hash 回填到 Task 7.2 新增 feature 的 `evidence`（可作为最后一次 `--amend` 或单独 `chore(state): backfill tidy evidence` commit）。
- **GOTCHA**: 若用 `--amend` 改最后一个 commit，确认未 push（当前确未 push），amend 安全。
- **VALIDATE**: `git log --oneline -1 && git status --porcelain | wc -l`  期望工作树仍为 0

---

## 测试策略

本功能是仓库卫生整理，无新业务逻辑，"测试"=确保零回归 + 无隐私泄漏 + 每次提交后可编译。

### 单元测试
- 复用现有核心包测试：`go test ./internal/chatlog/dailyreport ./internal/chatlog/semantic ./internal/chatlog/temporalgraph`。已实测确认 dailyreport 包无 "今日" 硬编码断言，字符串改动不影响测试。

### 集成测试
- `go build ./...` 在每个产品代码 commit 后运行，保证 HEAD 始终可编译（避免重演"HEAD 不编译"历史问题）。
- `go run . serve --help` 验证新 serve 子命令注册成功。

### 边界情况
- **私密 symlink**：`reports` 是 symlink 而非目录，`reports/` 规则失效 → 必须用 `/reports`。执行后 `git check-ignore reports` 必须命中。
- **暂存区污染**：每个 commit 前确认暂存区只含目标文件，杜绝 `git add -A` 把 `reports`/`reports.backup-*` 带入。
- **harness 字符串依赖**：progress.md/session-handoff.md 修正时不可删除 harness `checkIncludes` 依赖的小节标题。
- **active_feature_id 失配**：修正 feature_list.json 后 active id 必须匹配真实 feature，否则 harness fail。

---

## 验证命令

执行每个 Task 后立刻运行其 VALIDATE。阶段性 / 收尾全量门禁：

### 级别 1：语法与风格
```bash
go build ./...
bash -n scripts/chatlog-ha-guard.sh
python3 -m py_compile scripts/ralph/ralph.py scripts/ralph/dashboard.py
```

### 级别 2：单元测试
```bash
go test ./internal/chatlog/dailyreport ./internal/chatlog/semantic ./internal/chatlog/temporalgraph
```

### 级别 3：harness 集成
```bash
node scripts/check-root-harness.mjs            # 期望 80/80
node skills/chatlog-http-cli/scripts/check-harness-skill.mjs
```

### 级别 4：手动验证
```bash
git status --porcelain=v1 | wc -l              # 期望 0
git check-ignore reports reports.backup-20260606_124656/ scripts/ralph/__pycache__/ralph.cpython-314.pyc
go run . serve --help | head -3
git log --oneline -12
```

### 级别 5：附加（可选）
```bash
./init.sh                                      # 最小诚实门禁（若环境允许）
```

---

## 验收标准

- [ ] `git status --porcelain` 为空（所有项要么已提交，要么被 .gitignore）
- [ ] `git check-ignore reports`、`reports.backup-*/`、`__pycache__/*.pyc` 均命中（私密/垃圾确实被忽略）
- [ ] `reports`(symlink) 与 `reports.backup-*` **未**出现在任何 commit 的 `--name-only` 中
- [ ] `openai_prmpt.md` 已删除（`/tmp` 有备份）
- [ ] 每个产品代码 commit 后 `go build ./...` exit 0
- [ ] `go run . serve --help` 显示 serve 子命令
- [ ] 核心包 `go test` 全 ok
- [ ] `node scripts/check-root-harness.mjs` = 80/80
- [ ] `feature_list.json` 的 `active_feature_id` 指向新 `workspace-tidy-2026-06-17` 且匹配；db-runtime 标 `discarded`
- [ ] progress.md / session-handoff.md 保留 harness 依赖小节标题
- [ ] 全程无 `push`、无新分支、无 `reset`/`rebase`、无脚本批量删除
- [ ] 无聊天正文 / 真实 key 被打印或提交

## 完成检查清单

- [ ] Phase 1-8 所有 Task 按序完成，每个 VALIDATE 当场通过
- [ ] 级别 1-3 验证命令全绿
- [ ] 手动验证确认 serve 子命令可用、工作树干净
- [ ] 最终 `git log` 记录已留档，evidence 已回填 feature_list.json
- [ ] 回滚锚点（起始 HEAD `2f891fd8` + reflog）已记录

## 备注

- **为何在 main 直接提交而不开分支**：当前 main 已领先 origin/main 9 个未 push 的 commit，项目历史就是在 main 累积本地提交；用户本轮刚清理完分支混乱，再开分支与意图相悖。回滚靠 commit hash + reflog。
- **为何 openai_prmpt.md 删除而非忽略**：用户 memory 明确"不内置 AI 博主人设"，且本轮决策选"删除"。已留 /tmp 备份兜底。
- **为何 db-runtime 标 discarded 而非删除该 feature 条目**：保留历史可追溯（archive 有完整快照）；删条目会丢失"曾尝试过 DB 恢复但放弃"的记录。
- **DB 未恢复是已知遗留**：本计划不碰 US-004（破坏性运行态恢复），DB 仍不可查询，需用户 same-turn 授权才能处理，与本整理任务正交。
- **提交粒度偏细是刻意的**：适配弱循环模型逐步执行 + 每步可独立验证/回滚；若由人工执行可酌情合并相邻 chore commit。

---

**信心分数：9/10**（一次执行成功的把握）
- 加分：所有路径/编译依赖/harness 约束已实测确认；每 Task 配可执行 VALIDATE；私密项已定位并给出精确忽略规则；两个潜在风险（dailyreport 是否硬编码"今日"、render-enhanced 是否被 build 覆盖）已实测排除。
- 扣分：Task 7.2/7.3/7.4 状态文件修正是内容性判断（非纯机械），弱模型可能写出不满足 harness 字符串依赖的内容（缓解：每个修正 Task 都配了 grep/python 断言型 VALIDATE）；openai_prmpt.md 删除不可逆（已用 /tmp 备份缓解）。
