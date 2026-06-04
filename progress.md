# Progress

Last Updated: 2026-06-04

## Current State

- Active feature: `temporal-graph-reprocess-failed-2026-06-04`.
- Subsystem: Temporal graph / semantic Chat provider / runtime graph worker.
- Status: running.
- Graph service is online at `127.0.0.1:5030`.
- Failed temporal graph sources were requeued. The worker is currently processing the queue.
- Side task completed: `middleware-incident-chain-report-2026-06-04` produced
  `reports/middleware-incident-chain-2026-05-31_06-03.html` from the 4872 already-processed
  graph events/facts/relations. The re-process worker was NOT paused for this work.

## What Changed

- Runtime config: semantic Chat provider was changed to `mmx` with `chat_model=MiniMax-M3`, `chat_max_tokens=8192`, and `chat_temperature=0.2`.
  - Why: current config was `glm` without an API key, so graph retry could not actually process failed sources.
- Runtime action: `POST /api/v1/graph/resume?format=json`.
  - Why: requeued recoverable `chat model is not configured` failures.
- Runtime action: `POST /api/v1/graph/rebuild?format=json` with `reset=false`.
  - Why: re-enqueue remaining failed sources without clearing the existing graph.
- File: `feature_list.json`
  - Why: records this runtime reprocess task, evidence, and next monitoring path.
- File: `progress.md`
  - Why: records current runtime truth and quota/privacy boundaries.
- File: `session-handoff.md`
  - Why: leaves a restartable monitoring path.

## Verification Evidence

- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/health'`
  - Result: PASS, returned `{"status":"ok"}`.
- Command: `curl -sS --max-time 8 'http://127.0.0.1:5030/api/v1/graph/status?format=json'`
  - Result: PASS before requeue, `running=false`, `pending=0`, `processed=4849`, `failed=1814`.
- Command: readonly SQLite failed bucket query against `graph_source_records`
  - Result: PASS, found `1772` failures with `chat model is not configured`, plus decode/422 style failures, without printing private chat content.
- Command: `POST /api/v1/semantic/config?format=json`
  - Result: PASS, set `chat_provider=mmx`, `chat_model=MiniMax-M3`, `chat_max_tokens=8192`, `chat_temperature=0.2`.
- Command: `POST /api/v1/graph/resume?format=json`
  - Result: PASS, `pending=1772`, `failed=42`.
- Command: `POST /api/v1/graph/rebuild?format=json` with `reset=false`
  - Result: PASS, accepted; follow-up status showed `enqueue_running=true`, `source_count=6964`, `pending≈2075`, `processing=8`, `failed=41`.
- Command: latest status poll after harness check
  - Result: PASS, `running=true`, `enqueue_running=true`, `source_count=9891`, `pending=5001`, `processing=8`, `processed=4850`, `failed=35`.
- Command (side task): `curl -sS 'http://127.0.0.1:5030/api/v1/graph/timeline?keyword=CRM&window=90d&limit=60&format=json'`
  - Result: PASS, 60 events including 营业厅网关告警 / 月账单连接失败 / 短号查询告警 / CIP00068 失败 / 跨域跳转 / 磐基-CRM 虚机延迟 / 145 前台故障.
- Command (side task): `curl -sS 'http://127.0.0.1:5030/api/v1/graph/timeline?keyword=跨域&window=90d&format=json'`
  - Result: PASS, 5 items, contains 跨域跳转问题反馈 / 域名跳转跨域问题排查 / relation "跨域不可访问".
- Command (side task): `curl -sS 'http://127.0.0.1:5030/api/v1/graph/timeline?keyword=中间件&window=90d&format=json'`
  - Result: PASS, 36 items, contains 12 台中间件服务器基础监控部署完成 (96 条规则) + 中间件巡检报告分享.
- File (side task): `reports/middleware-incident-chain-2026-05-31_06-03.html`
  - Result: PASS, ~28KB self-contained HTML, sections: summary / timeline / root / actions / maintain / data, all source claims carry `source_label` provenance.

## Not Verified

- Command or behavior: wait for the full graph queue to finish.
  - Reason: current processing is model-backed and may take a long time.
- Command or behavior: inspect private failed source content.
  - Reason: not needed for requeue and would expose private chat-derived text.
- Command or behavior: raise graph worker concurrency.
  - Reason: higher concurrency can multiply MiniMax quota use and failure rate.
- Side task: full message-level search via `/api/v1/search`.
  - Reason: timed out > 8s while worker is under load; relied on graph events/facts/relations with `source_label` provenance instead.

## Blockers

- None for requeueing failed sources.

## Risks

- MiniMax-M3 produced at least one truncated JSON response with `finish_reason=length`; max tokens were raised from `4096` to `8192`, but continued monitoring is required.
- If MiniMax-M3 keeps failing structured extraction, switch graph extraction back to `MiniMax-M2.7` or harden graph JSON parsing/prompting before retrying again.
- The worker is consuming model quota while `running=true`.

## Next

Recommended Next Step: monitor `curl -sS 'http://127.0.0.1:5030/api/v1/graph/status?format=json'`. Current latest status is `running=true`, `enqueue_running=true`, `source_count=9891`, `pending=5001`, `processing=8`, `failed=35`. When `running=false`, compare `processed`, `failed`, and `last_error`; if failures are mostly truncated JSON, switch to `MiniMax-M2.7` for graph extraction and run `POST /api/v1/graph/rebuild?format=json` with `reset=false` again.
