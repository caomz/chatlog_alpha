# PRD: Ralph 分支生命周期 — 最后自动合并代码提交分支

## 背景

`scripts/ralph/prd.json` 一直携带 `branchName` 字段（如 `ralph/graph-knowledge-digest`），但 `scripts/ralph/ralph.py` 从未使用它：循环始终在当前分支（实际是 main）上逐 story 自动提交，结束时没有任何分支合并环节。结果是 feature 提交直接散落在 main 上，无法按 run 审计，也无法在 run 失败时整体放弃。

## 目标

1. Ralph 启动时按 `prd.json.branchName` 创建/切换工作分支，story 提交落在工作分支上。
2. 全部 story `passes=true`（且无 blocked）后，ralph.py 自动把工作分支合并回启动时所在的 base 分支（`--no-ff`，保留 run 痕迹）。
3. 任何 blocked story 存在时跳过合并；合并冲突时安全 abort 并保留工作分支。
4. 绝不自动 `push`；`RALPH_AUTO_MERGE` 环境变量可关闭合并（默认开启），与既有 `RALPH_AUTO_COMMIT` 风格一致。

## 非目标

- 不做远端分支/PR 自动化（不 push、不开 PR）。
- 不改 developer/validator agent 的"禁止直接 merge"边界——合并只由 ralph.py 执行。
- 不修改根 `AGENTS.md`（启动前已 dirty，属用户文件）。

## 安全约束

- 合并前后不得丢失启动前已有的 dirty 工作区文件。
- 沙箱测试必须在 `mktemp -d` 一次性 git 仓库内执行，绝不对真实仓库做写操作。
- 合并成功后保留工作分支（审计需要），只打印手动清理提示。
