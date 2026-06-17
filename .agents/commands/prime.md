---
description: 为代理建立代码库理解
---

# Prime：加载项目上下文

## 目标

通过分析代码库结构、文档和关键文件，建立对项目的全面理解。

## 流程

### 1. 分析项目结构

列出所有被追踪的文件：
!`git ls-files`

显示目录结构：
在 Linux 上运行：`tree -L 3 -I 'node_modules|__pycache__|.git|dist|build'`

### 2. 阅读核心文档

- 阅读 `scripts/ralph/prd.json` 或具体需求/规格文件
- 阅读 `AGENTS.md`、`CLAUDE.md` 和 `skills/chatlog-http-cli/SKILL.md`
- 阅读项目根目录和主要目录下的 README 文件
- 阅读任何架构相关文档
- 阅读 `feature_list.json`、`progress.md`、`session-handoff.md` 理解当前运行态和边界

### 3. 识别关键文件

基于项目结构，识别并阅读：
- 主入口文件（如 `main.py`、`index.ts`、`app.py` 等）
- 核心配置文件（如 `go.mod`、`Makefile`、`init.sh`）
- 关键模型 / schema 定义
- 重要的 CLI command、HTTP handler、daily report、semantic、temporal graph 或 WeChat DB 文件

### 4. 理解当前状态

检查最近活动：
!`git log -10 --oneline`

检查当前分支和状态：
!`git status`

## 输出报告

提供一份简明总结，覆盖以下内容：

### 项目概览
- 应用的目的和类型
- 主要技术和框架
- 当前版本 / 状态

### 架构
- 整体结构和组织方式
- 已识别出的关键架构模式
- 重要目录及其用途

### 技术栈
- 语言及版本
- 框架和主要库
- 构建工具和包管理器
- 测试框架

### 核心原则
- 观察到的代码风格和约定
- 文档规范
- 测试方法

### 当前状态
- 当前活跃分支
- 最近的改动或当前开发重点
- `feature_list.json` 中的 `active_feature_id`
- 是否存在启动前 dirty 文件
- 任何需要立即注意的观察或问题

**让这份总结易于快速浏览：使用清晰的标题和项目符号列表。**
