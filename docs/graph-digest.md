# Graph Digest 时间窗口摘要

一句话结论：`chatlog report graph` 通过常驻 HTTP 服务，把时间图谱的实体/事件/事实聚合成 Obsidian 友好的 Markdown，不触发 SQLite 直读，不进 git。

## 命令用法

```bash
# 默认：最近 7 天，无模型调用
chatlog report graph

# 指定时间窗口
chatlog report graph --days 30

# 开启模型综述（消耗额度，见下方隐私边界）
chatlog report graph --days 7 --summary

# 指定非默认服务地址
chatlog report graph --base-url http://127.0.0.1:5031
```

前提：`chatlog serve`（或 `chatlog http start`）已运行，服务默认监听 `http://127.0.0.1:5030`。

成功输出（仅元数据，不打印摘要正文）：

```text
path: /path/to/reports/graph-digest-2026-06-04_2026-06-11.md
window_start: 2026-06-04T00:00:00+08:00
window_end: 2026-06-11T12:34:56+08:00
entity_count: 12
event_count: 47
fact_count: 8
relation_count: 23
summary_used: false
```

## 参数说明

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--days` | `7` | 时间窗口长度（天），从当前时刻往前推 |
| `--summary` | `false` | 是否调用 Chat 模型生成综述章节（消耗额度） |
| `--base-url` | `http://127.0.0.1:5030` | chatlog HTTP 服务地址 |

底层 HTTP 端点：`POST /api/v1/graph/digest?days=N&format=json[&summary=true]`

也可直接用 curl 调用：

```bash
curl -s -X POST 'http://127.0.0.1:5030/api/v1/graph/digest?days=7&format=json'
```

## 同窗口幂等覆盖策略

输出文件名为 `graph-digest-<start>_<end>.md`（精确到天），同窗口多次调用会覆盖同一文件，不报错，不增量追加。这是有意设计：

- 同一天多次执行（修复数据、调整模型配置后重跑）不会产生重复文件。
- 文件修改时间可作为"最后生成时间"的可靠依据。

## quota 与隐私边界

**默认零模型调用**：不加 `--summary` 时，摘要完全由图谱 SQLite 聚合生成，不发送任何内容到外部服务。

**`--summary` 时的隐私保护**：
- 发送给模型的 prompt 只含：时间范围、实体名称 + 出现次数、事件/事实/关系的计数。
- 不含原始 talker_id / sender_id、消息正文、群聊名称等私密标识。
- prompt 保持角色中立，不内置任何职业身份设定。
- 模型调用失败时摘要降级为模板内容，`summary_used: false`，命令仍以 exit 0 完成。

**不进 git**：`reports/graph-digest-*.md` 已在 `.gitignore` 覆盖范围（`reports/` 目录）或通过 git status 人工确认，永不提交。
