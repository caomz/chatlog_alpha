# Chatlog Harness Map

一句话结论：`chatlog_alpha` 是微信 4.x 聊天记录本地查询工具，当前仓库必须成为 agent 的唯一事实来源。

## Project Identity

- Language/runtime: Go module `github.com/sjzar/chatlog`
- Main CLI: `main.go`
- Command tree: `cmd/chatlog/`
- Core application context/config: `internal/chatlog/ctx/`, `internal/chatlog/conf/`
- HTTP service and API routing: `internal/chatlog/http/`
- WeChat database access: `internal/wechatdb/`
- Utilities and platform support: `pkg/`

## High-Risk Domains

- Private chat data and generated reports under `reports/`
- WeChat database keys and image/media keys
- External model/API quota for `--vision`, `--summary`, MiniMax/MMX, GLM, DeepSeek, Ollama, or OpenAI-compatible providers
- Local service state around `127.0.0.1:5030`
- macOS permissions for WeChat memory/database access

## Fact Source Rules

- If a decision matters, put it near the code or in a repo-local doc.
- Do not rely on chat history as the only record of architecture, verification, or status.
- Prefer compact, current docs over large stale docs.
- Stale docs are worse than missing docs because they make agents confidently wrong.

## Five Fresh-Session Questions

A fresh session should answer these from the repo:

1. What does this project do?
2. How do I run it locally?
3. How do I verify a change?
4. Where is the subsystem I need to change?
5. What privacy/quota risks apply?
