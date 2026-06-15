# Technology Stack

**Analysis Date:** 2026-06-15

## Languages

**Primary:**
- Go `1.24.0` (declared in `go.mod:3`) — All production code. cgo is required for the default build path because of `github.com/mattn/go-sqlite3` (`Makefile:35`, `.goreleaser.yaml:14`, `init.sh:35-69`).

**Secondary:**
- JavaScript (Node) — `scripts/check-root-harness.mjs`, `skills/chatlog-http-cli/scripts/check-harness-skill.mjs`, and `.agents/skills/*` automation scripts.
- Python — Helper scripts invoked from Go via `internal/chatlog/hermespush/hermes_weixin_bridge.py` and `hermes_qq_bridge.py`; also Ralph automation under `scripts/ralph/`.
- Protocol Buffers — Generated types under `internal/model/wxproto/` (compiled via `google.golang.org/protobuf`).

## Runtime

**Environment:**
- Go 1.24.0 toolchain (driven by `go.mod`).
- macOS (arm64 + amd64) and Windows (amd64 cgo, arm64 non-cgo) are first-class targets; `.goreleaser.yaml:15-20` and `.github/workflows/release.yml:9-110` both build all four combinations.
- sqlite3 runtime via cgo (`mattn/go-sqlite3`).

**Package Manager:**
- Go modules — `go.mod` / `go.sum` present.
- Lockfile: `go.sum` (committed).

## Frameworks

**Core:**
- `github.com/spf13/cobra v1.9.1` — CLI framework; used by `cmd/chatlog/root.go` and every `cmd/chatlog/cmd_*.go`.
- `github.com/spf13/viper v1.20.1` — Configuration loader (`pkg/config/config.go`, `pkg/config/types.go`); see env-prefix `CHATLOG` in `internal/chatlog/conf/conf.go:16`.
- `github.com/gin-gonic/gin v1.10.1` — HTTP API framework; routes defined in `internal/chatlog/http/route.go`.
- `github.com/mark3labs/mcp-go v0.38.0` — MCP-style tool server registered in `internal/chatlog/http/mcp.go`.
- `github.com/rivo/tview v0.0.0-20250625164341-a4a78f1e05cb` + `github.com/gdamore/tcell/v2 v2.8.1` — Terminal UI used by `internal/ui/**` and `internal/chatlog/app.go`.

**Testing:**
- Standard library `testing` package (e.g. `internal/chatlog/conf/semantic_test.go`, `internal/chatlog/temporalgraph/store_test.go`).
- External assertion libraries: none observed; tests rely on stdlib.
- `Makefile:31` runs `go test ./... -cover`.

**Build/Dev:**
- `Makefile` (`make build`, `make crossbuild`, `make test`, `make lint`) with `golangci-lint run ./...`.
- `init.sh` — quick/full/runtime verification gate (no harness build, runs `node scripts/check-root-harness.mjs`, `go run . report daily --help`, etc.).
- `.goreleaser.yaml` v2 — release pipeline (`goos: darwin, windows`, `goarch: arm64, amd64`, `CGO_ENABLED=1`).
- `.github/workflows/release.yml` — CI for both `macos-latest` and `windows-latest` (uses MSYS2/MinGW64 for Windows cgo amd64).

## Key Dependencies

**Critical (product-defining):**
- `github.com/mattn/go-sqlite3 v1.14.32` — SQLite access for both local WeChat data (`internal/wechatdb/`) and the in-process temporal-graph / semantic stores (`internal/chatlog/temporalgraph/store.go:13`, `internal/chatlog/database/`).
- `github.com/sjzar/go-lame v0.0.9` + `github.com/sjzar/go-silk v0.0.1` — WeChat silk voice decoding to MP3; combined in `pkg/util/silk/silk.go`.
- `github.com/Eyevinn/mp4ff v0.49.0` — MP4/HEVC/AVC parsing for WeChat `.dat` -> MP4 conversion in `pkg/util/dat2img/wxgf.go`.
- `github.com/xuri/excelize/v2 v2.10.0` — XLSX export used by `internal/chatlog/http/route.go:22` (export endpoints).
- `github.com/shirou/gopsutil/v4 v4.25.7` — WeChat process detection in `pkg/process/process.go` plus `internal/wechat/process/{darwin,windows}/`.
- `github.com/fsnotify/fsnotify v1.9.0` — File-system watcher used by `pkg/filemonitor/` and `internal/chatlog/wechat/service.go` to trigger auto-decryption on new DB writes.
- `howett.net/plist v1.0.1` — Plist parsing on macOS for the WeChat version detection in `pkg/appver/version_darwin.go`.
- `google.golang.org/protobuf v1.36.7` — Generated wxproto types under `internal/model/wxproto/`.
- `github.com/google/uuid v1.6.0` — UUIDs in `pkg/util/dat2img/wxgf.go` and Hermes Weixin message ids.
- `github.com/cespare/xxhash v1.1.0` — Fast hashing in `pkg/filecopy/filecopy.go`.
- `github.com/klauspost/compress v1.18.0` — `zstd` decoder at `pkg/util/zstd/zstd.go`.
- `github.com/mitchellh/mapstructure v1.5.0` + `github.com/go-viper/mapstructure/v2 v2.4.0` — Config decoding used through `pkg/config/types.go` and the semantic config structs.

**Observability & Logging:**
- `github.com/rs/zerolog v1.34.0` — Primary logger used across `internal/chatlog/**` (`manager.go`, `service.go`, `temporalgraph/manager.go`, etc.).
- `github.com/sirupsen/logrus v1.9.3` — Secondary logger wired up only at CLI startup in `cmd/chatlog/log.go:13`.

**MCP & HTTP Middleware:**
- `github.com/gin-contrib/sse v1.1.0`, `github.com/go-playground/validator/v10 v10.27.0`, `github.com/goccy/go-json v0.10.5`, `github.com/bytedance/sonic v1.14.0` — Gin’s HTTP/JSON/validation stack (transitive but load-bearing).

**Optional / Forward-looking:**
- `github.com/Eyevinn/mp4ff` plus `internal/chatlog/http/wasm/wasm_video_decode.{js,wasm}` and `wasm_keystream_helper.js` — embedded JS+WASM keystream helpers for in-browser video decode.

## Configuration

**Environment:**
- Env prefix: `CHATLOG` (see `internal/chatlog/conf/conf.go:16`).
- Config dir override: `CHATLOG_DIR`.
- Config files: managed by viper; defaults written through `pkg/config/default.go` and `internal/chatlog/conf/conf.go`.
- Two scopes:
  - TUI config — `internal/chatlog/conf/tui.go` (loaded via `LoadTUIConfig`).
  - Server config — `internal/chatlog/conf/server.go` (loaded via `LoadServiceConfig`, also reads `chatlog.json` next to the data dir).
- Secrets are scrubbed before log emission (`internal/chatlog/conf/conf.go:116-144` — keys like `datakey`, `imgkey`, `apikey`, `clientsecret`, `accesstoken`, `refreshtoken`, `password`, anything containing `token`/`secret`/`apikey`).

**Build:**
- `Makefile`, `.goreleaser.yaml`, `init.sh` — primary build/test/lint entrypoints.
- `go test ./... -cover` for tests.
- `golangci-lint run ./...` for lint.

## Platform Requirements

**Development:**
- macOS arm64 (primary dev box — this repo's working dir is `darwin`).
- Go 1.24.0 toolchain.
- CGO-capable compiler (Xcode CLT on macOS, MinGW64 gcc on Windows per `.github/workflows/release.yml:67-72`).
- Optional: local Ollama daemon for default semantic config (`internal/chatlog/conf/semantic.go:11`).
- Optional: Hermes push daemon (`hermes` binary on `$PATH`) for the WeChat/QQ push integrations.

**Production:**
- Desktop binary distribution via GitHub Releases (`softprops/action-gh-release@v2`, archive formats: zip / binary per `.goreleaser.yaml:23-39`).
- Local-only deployment model — runs as a CLI/TUI or as a long-lived HTTP daemon (`0.0.0.0:5030` default in `internal/chatlog/conf/server.go:6`).
- No server-side infrastructure: SQLite files live under `<workdir>` and `<datadir>` selected by the user.

---

*Stack analysis: 2026-06-15*