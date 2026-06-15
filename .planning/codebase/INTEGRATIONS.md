# External Integrations

**Analysis Date:** 2026-06-15

## APIs & External Services

**Semantic / LLM providers (chat, embeddings, rerank, vision):**

| Provider | Endpoints | Env / config key | File / constant |
|----------|-----------|------------------|-----------------|
| Ollama (local) | `POST /api/embed`, `POST /api/generate`, `POST /api/chat` | `ollama_base_url` (default `http://127.0.0.1:11434`) | `internal/chatlog/conf/semantic.go:11`, `internal/chatlog/semantic/client.go:190-230, 333-404, 1217-1243, 1381-1419` |
| GLM / 智谱 (OpenAI-compatible) | `POST /embeddings`, `POST /rerank`, `POST /chat/completions` (streaming) | `base_url` (default `https://open.bigmodel.cn/api/paas/v4`), `api_key` | `internal/chatlog/conf/semantic.go:17-21`, `internal/chatlog/semantic/client.go:150-188, 264-331, 790-836, 1421-1527` |
| DeepSeek (OpenAI-compatible) | `POST /chat/completions` | `deepseek_api_key`, `deepseek_base_url` (default `https://api.deepseek.com`), chat_model default `deepseek-chat` | `internal/chatlog/conf/semantic.go:22-23`, `internal/chatlog/semantic/client.go:739-749, 778-786` |
| MiniMax (`mmx` provider) | `POST {base}/chat/completions`, `POST {base}/v1/coding_plan/vlm` (vision) | `MINIMAX_API_KEYS` (comma-separated, also `MINIMAX_API_KEY`, `MINIMAXn_API_KEY`), `MINIMAX_BASE_URL` / `MINMAX_BASE_URL`, or `~/.mmx/config.json` | `internal/chatlog/semantic/client.go:35-43, 838-1009, 1135-1215` |

The MiniMax integration maintains a privacy-safe API key pool (`miniMaxAPIKeyPool` in `internal/chatlog/semantic/client.go:438-720`) with leases, retries, quarantine buckets, and a public `/api/v1/semantic/mmx/status` endpoint (`internal/chatlog/http/route.go:82`) that returns counts and labels only — never real keys.

**Hermes push (cross-platform notification):**

| Channel | Endpoint / path | Auth | File |
|---------|-----------------|------|------|
| WeChat iLink bot (text) | `POST {base_url}/ilink/bot/sendmessage` (default `https://ilinkai.weixin.qq.com`) | `Bearer {WEIXIN_TOKEN}`, headers `AuthorizationType: ilink_bot_token`, `iLink-App-Id: bot`, `X-WECHAT-UIN`, `iLink-App-ClientVersion` | `internal/chatlog/hermespush/hermes_weixin.go:24-30, 213-269` |
| WeChat media (via Hermes Python helper) | `internal/chatlog/hermespush/hermes_weixin_bridge.py` invoked as subprocess | Reads `.env`, `config.yaml`, `weixin/accounts/<id>.json`, `channel_directory.json` under `HERMES_HOME` or `~/.hermes` | `internal/chatlog/hermespush/hermes_weixin.go:289-431` |
| QQ bot (via Hermes Python helper) | `hermes_qq_bridge.py` subprocess | `QQ_APP_ID`, `QQ_CLIENT_SECRET`, `QQBOT_HOME_CHANNEL`, `QQBOT_HOME_CHANNEL_NAME` from `.env` | `internal/chatlog/hermespush/hermes_qq.go:44-79` |

Hermes installation detection uses `exec.LookPath("hermes")` and the `HERMES_HOME` env var (`internal/chatlog/hermespush/hermes_weixin.go:115-136, 289-324`).

**HTTP-facing outbound integrations:**
- `POST_URL` configured per message-hook (`conf.MessageHook.PostURL`) — arbitrary webhook target for keyword / forward-all events. See `internal/chatlog/http/route.go:153-242` and `internal/chatlog/messagehook/service.go`.

## Data Storage

**Databases (all local SQLite via `mattn/go-sqlite3`):**
- WeChat data — Decrypted copies of the WeChat 4.x databases under the user's `data_dir`. Access layer in `internal/wechatdb/` (`datasource/`, `repository/`, `wcdbapi/`).
- Semantic index — `semantic_index.db` (gitignored; see `.gitignore:87-88`). Used by `internal/chatlog/semantic/store.go`.
- Temporal graph store — SQLite-backed source queue, entities, facts, events, relations, timeline and QA; `internal/chatlog/temporalgraph/store.go:13`.
- Daily report / Hermes state — local files under the chatlog working directory.

**File Storage:**
- Local filesystem only. Decrypted media are saved under the configured `work_dir` when `save_decrypted_media` is true (default `internal/chatlog/conf/server.go:27-29, 85-87`).
- Chat-derived reports and digests land in `reports/graph-digest-<start>_<end>.md` and `reports/daily-report-*` — these are gitignored (`.gitignore:67`).

**Caching:**
- None (no Redis/Memcache). All caches are in-process maps (e.g. `miniMaxAPIKeyPool` in `internal/chatlog/semantic/client.go`).
- The HTTP service exposes `POST /api/v1/cache/clear` (`internal/chatlog/http/route.go:136`) for operator-triggered eviction of in-process caches.

## Authentication & Identity

**Operator auth on the HTTP API:** None. The `gin` engine has no auth middleware — `internal/chatlog/http/route.go` registers `/health`, `/api/v1/*` without any token/cookie check; `/api/v1` is gated only by `s.checkDBStateMiddleware()` (`internal/chatlog/http/middleware.go`). The product is intended for local/desktop use and binds `0.0.0.0:5030` by default.

**Model-provider auth:**
- GLM / DeepSeek / MiniMax — static `Bearer` tokens passed via `Authorization` header (`internal/chatlog/semantic/client.go:1488, 1605-1607`).
- Ollama — no auth (header omitted by `doJSONNoAuth`).

**Hermes auth:**
- WeChat: token from `WEIXIN_TOKEN` env / `config.yaml` `platforms.weixin.token` / `weixin/accounts/<id>.json`. Channel ids come from `WEIXIN_HOME_CHANNEL` or `channel_directory.json`.
- QQ: client-credentials pair from `QQ_APP_ID` / `QQ_CLIENT_SECRET`.

## Monitoring & Observability

**Error Tracking:** None. No Sentry / Bugsnag / Datadog integration observed.

**Logs:**
- Structured logger `github.com/rs/zerolog/log` is the runtime default across `internal/chatlog/**`. Configuration logs scrub secrets (`internal/chatlog/conf/conf.go:116-144`).
- Secondary logger `github.com/sirupsen/logrus` is wired up only at CLI startup (`cmd/chatlog/log.go:13`).
- MiniMax error path emits privacy-safe buckets (e.g. `minimax_auth_error`, `minimax_sensitive_1026`, `minimax_rate_limited`) instead of raw upstream messages (`internal/chatlog/semantic/client.go:1057-1089`).

## CI/CD & Deployment

**Hosting / Distribution:**
- GitHub Releases via `softprops/action-gh-release@v2` (`releases:latest`, prerelease: true, `make_latest: false`).
- Archives: zip + raw binary per platform/arch pair.

**CI Pipeline:**
- `.github/workflows/release.yml` — single workflow with three jobs:
  - `build-macos` (macos-latest) builds `darwin/amd64` and `darwin/arm64`.
  - `build-windows` (windows-latest) builds `windows/amd64` (CGO via MSYS2 MinGW64) and `windows/arm64` (non-CGO fallback).
  - `publish` aggregates artifacts and pushes to GitHub Releases.
- Local verification gate: `init.sh` (quick / full / runtime modes) plus `scripts/check-root-harness.mjs` and `scripts/chatlog-ha-guard.sh` for HA MiniMax key-pool sanity.

## Environment Configuration

**Required env vars (semantic / model providers):**

| Variable | Purpose | Default if unset |
|----------|---------|------------------|
| `CHATLOG_DIR` | Config directory override | platform default config dir |
| `CHATLOG_*` | All server / TUI config keys (env prefix) | viper-driven defaults |
| `MINIMAX_API_KEYS` (or `MINIMAX_API_KEY`, `MINIMAXn_API_KEY`, `MINMAX_API_KEY`, `MINMAXn_API_KEY`) | MiniMax provider API keys (comma-separated list preferred) | falls back to `~/.mmx/config.json` |
| `MINIMAX_BASE_URL` / `MINMAX_BASE_URL` | MiniMax base URL | `https://api.minimaxi.com/v1` (CN) or `https://api.minimax.io/v1` (intl) by region |
| `HERMES_HOME` | Hermes push installation root | falls back to `~/.hermes`, then `~/.hermes/profiles/*` |
| `WEIXIN_HOME_CHANNEL`, `WEIXIN_HOME_CHANNEL_NAME`, `WEIXIN_ACCOUNT_ID`, `WEIXIN_TOKEN`, `WEIXIN_BASE_URL`, `WEIXIN_CDN_BASE_URL` | Hermes WeChat channel config | read from `<HERMES_HOME>/.env`, `config.yaml`, or `channel_directory.json` |
| `QQ_APP_ID`, `QQ_CLIENT_SECRET`, `QQBOT_HOME_CHANNEL`, `QQBOT_HOME_CHANNEL_NAME` | Hermes QQ bot config | read from `<HERMES_HOME>/.env` |
| `RALPH_AUTO_MERGE` | Toggles automatic merge after Ralph story completion (`0/false/no` disables) | `1` |

**Secrets location:**
- Local only. Config files live in the chatlog config dir (managed by viper, e.g. `chatlog-server.yaml`).
- Sensitive keys (`datakey`, `imgkey`, `api_key`, `client_secret`, `access_token`, `refresh_token`, `password`, anything matching `*token*`/`*secret*`/`*apikey*`) are scrubbed from log output (`internal/chatlog/conf/conf.go:135-144`).
- WeChat `data_key` and `img_key` may also be read from a `chatlog.json` next to the data dir (`internal/chatlog/conf/conf.go:73-90`).

## Webhooks & Callbacks

**Incoming:**
- None. The HTTP service is read/command-driven by the local user / TUI; there are no public webhook endpoints to be called by third parties.
- Generic outgoing webhook target configurable via `message_hook.post_url` (`internal/chatlog/http/route.go:235`, `internal/chatlog/messagehook/service.go`).

**Outgoing:**
- Generic `POST` to `message_hook.post_url` with keyword-triggered / forward-all chat context payloads (`internal/chatlog/messagehook/service.go`).
- Hermes WeChat text send — `POST {base_url}/ilink/bot/sendmessage` (`internal/chatlog/hermespush/hermes_weixin.go:213-269`).
- Hermes WeChat/QQ media send — Go invokes `hermes_weixin_bridge.py` / `hermes_qq_bridge.py` as subprocesses (`internal/chatlog/hermespush/hermes_weixin_bridge.py`, `hermes_qq_bridge.py`).

---

*Integration audit: 2026-06-15*