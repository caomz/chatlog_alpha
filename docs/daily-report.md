# 每日微信日报

一句话结论：`chatlog` 可以生成当天群聊 `@caomz` 汇总、回复状态、私聊更新，并输出 Markdown / JSON 文件。

## CLI

```bash
chatlog report daily \
  --date today \
  --mention caomz \
  --alias 曹明哲 \
  --before 5 \
  --after 10 \
  --reply-window 60 \
  --include-private \
  --format both \
  --out ./reports
```

输出：

```text
reports/daily-YYYY-MM-DD.md
reports/daily-YYYY-MM-DD.json
```

CLI 默认直接读取本地 `chatlog` service config 和数据库，不启动 HTTP 服务，也不会把完整聊天内容打印到 stdout。

如果只想调用正在运行的 HTTP 服务：

```bash
chatlog report daily --http http://127.0.0.1:5030 --date today --mention caomz --out ./reports
```

启用图片理解：

```bash
chatlog report daily \
  --date 2026-05-26 \
  --mention caomz \
  --alias 曹明哲 \
  --vision \
  --max-images 50 \
  --out ./reports
```

图片理解会调用当前 `semantic.chat_provider` 配置的 Chat 模型，可能消耗外部模型额度。结果会写入：

```text
reports/daily-YYYY-MM-DD.md
reports/daily-YYYY-MM-DD.json
reports/daily-YYYY-MM-DD-dialogue-analysis.md
reports/.image-analysis-cache.json
```

启用图片理解 + 文本解析，并让 MiniMax 多 Key 并发：

```bash
export MINIMAX_API_KEYS="sk-cp-***,sk-cp-***,sk-cp-***"
export MINIMAX_BASE_URL="https://api.minimaxi.com/v1"

chatlog report daily \
  --date 2026-05-26 \
  --mention caomz \
  --alias 曹明哲 \
  --vision \
  --summary \
  --max-images 50 \
  --analysis-concurrency 0 \
  --out ./reports
```

`--analysis-concurrency 0` 会按 MiniMax API key 数量自动设置 worker 数。MiniMax 文本和图片请求共用同一个全局 key pool，同一个 key 同时只会承载一个请求。

## HTTP API

生成 JSON：

```bash
curl "http://127.0.0.1:5030/api/v1/daily/report?date=today&mention=caomz&format=json"
```

生成 Markdown：

```bash
curl "http://127.0.0.1:5030/api/v1/daily/report?date=today&mention=caomz&aliases=曹明哲&before=5&after=10&reply_window_minutes=60&format=markdown"
```

生成带图片理解的 Markdown：

```bash
curl "http://127.0.0.1:5030/api/v1/daily/report?date=2026-05-26&mention=caomz&aliases=曹明哲&vision=1&max_images=50&format=markdown"
```

生成带图片理解和文本解析的 Markdown：

```bash
curl "http://127.0.0.1:5030/api/v1/daily/report?date=2026-05-26&mention=caomz&aliases=曹明哲&vision=1&summary=1&max_images=50&analysis_concurrency=0&format=markdown"
```

保存日报：

```bash
curl -X POST "http://127.0.0.1:5030/api/v1/daily/report/save" \
  -H "Content-Type: application/json" \
  -d '{
    "date": "today",
    "mention": "caomz",
    "aliases": ["曹明哲"],
    "before": 5,
    "after": 10,
    "reply_window_minutes": 60,
    "include_private": true,
    "vision": true,
    "summary": true,
    "max_images": 50,
    "analysis_concurrency": 0,
    "out_dir": "./reports"
  }'
```

HTTP 保存接口会把相对路径限制在 `WorkDir` 下，拒绝 `../../` 这类路径穿越。

## 识别规则

- 群聊：`talker` 以 `@chatroom` 结尾，或消息模型标记为 `IsChatRoom`。
- @ 检测：匹配 `mention` 和 `aliases`，支持 `@caomz`、`@ caomz`、`@曹明哲`。
- 回复：同一群聊内，命中 @ 消息之后 `reply_window_minutes` 分钟内的 `is_self=true` 消息；遇到下一条 @ 命中或超过窗口即停止。
- 私聊：统计当天有消息的非群聊会话，排除公众号和常见系统号。

## 风险和限制

- `summary=1` 会调用配置的 Chat 模型，生成 @ 消息解析、群聊级总结和私聊摘要。
- 第一版不做 Web 页面、定时任务和 Hermes 推送。
- 日报可能包含隐私消息，`reports/` 已加入 `.gitignore`，不要提交真实日报。
- 图片理解会把图片内容发送给配置的视觉模型；如果使用 MiniMax / OpenAI-compatible 远端模型，需要先确认隐私和额度风险。
