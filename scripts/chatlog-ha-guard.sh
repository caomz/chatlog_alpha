#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ADDR="${CHATLOG_HA_ADDR:-http://127.0.0.1:5030}"
TMUX_TARGET="${CHATLOG_HA_TMUX_TARGET:-chatlog-alpha:0.0}"
TMUX_SESSION="${TMUX_TARGET%%:*}"
CONFIG_DIR="${CHATLOG_HA_CONFIG_DIR:-.cache/daily-report-config}"
EXPECTED_PROVIDER="${CHATLOG_HA_PROVIDER:-mmx}"
EXPECTED_MODEL="${CHATLOG_HA_MODEL:-MiniMax-M2.7}"
TARGET_WORKERS="${CHATLOG_HA_WORKERS:-5}"
TARGET_ENQUEUE_WORKERS="${CHATLOG_HA_ENQUEUE_WORKERS:-1}"
TARGET_KEY_COUNT="${CHATLOG_HA_KEY_COUNT:-5}"
RESUME_GRAPH=1
DRY_RUN=0
LOOP_INTERVAL=0

log() {
  printf '%s %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$*"
}

# plan_action appends a "would-do" hint to the planned-action summary. In
# non-loop runs it always prints, in --loop runs it is suppressed to keep
# each tick to one short line; the loop tick still reports action_taken
# based on whether any mutating helper actually ran.
plan_action() {
  PLANNED_ACTIONS+=("$1")
  if [ "$LOOP_INTERVAL" -eq 0 ]; then
    log "PLAN $*"
  fi
}

reset_planned() {
  PLANNED_ACTIONS=()
}

emit_summary_block() {
  local label="$1" action_taken="${2:-no}"
  log "SUMMARY label=$label health=$GUARD_HEALTH semantic=${SEMANTIC_PROVIDER:-unknown}/${SEMANTIC_MODEL:-unknown} has_api_key=${SEMANTIC_HAS_KEY:-unknown} mmx_configured=${MMX_CONFIGURED:-0} workers=${GRAPH_WORKERS:-0}/${GRAPH_ENQUEUE_WORKERS:-0} pending=${GRAPH_PENDING:-0} failed=${GRAPH_FAILED:-0} last_error=${GRAPH_LAST_ERROR:-none} action_taken=$action_taken planned_count=${#PLANNED_ACTIONS[@]}"
}

usage() {
  cat <<'USAGE'
Usage: scripts/chatlog-ha-guard.sh [--dry-run] [--no-resume] [--loop SECONDS]

Checks the local Chatlog HTTP service and heals common temporal graph drift:
  - restarts chatlog-alpha with ./bin/chatlog serve --config-dir .cache/daily-report-config
    when health/config is unavailable or semantic Chat config drifted away from MMX
  - resumes the graph only after Chat config is ready, requeueing recoverable config/timeout failures

Privacy boundary:
  Prints only status codes, counts, model names, and bucket counts. It never prints API keys,
  source content, model prompts, model output, or chat messages.

Environment:
  CHATLOG_HA_ADDR          default http://127.0.0.1:5030
  CHATLOG_HA_TMUX_TARGET   default chatlog-alpha:0.0
  CHATLOG_HA_CONFIG_DIR    default .cache/daily-report-config
  CHATLOG_HA_PROVIDER      default mmx
  CHATLOG_HA_MODEL         default MiniMax-M2.7
  CHATLOG_HA_WORKERS       default 5
  CHATLOG_HA_ENQUEUE_WORKERS default 1
  CHATLOG_HA_KEY_COUNT     default 5
  CHATLOG_HA_KEYS_FILE     default ~/.chatlog-ha-keys
USAGE
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --no-resume)
      RESUME_GRAPH=0
      shift
      ;;
    --loop)
      LOOP_INTERVAL="${2:-}"
      if ! [[ "$LOOP_INTERVAL" =~ ^[0-9]+$ ]] || [ "$LOOP_INTERVAL" -lt 5 ]; then
        echo "--loop requires an interval >= 5 seconds" >&2
        exit 2
      fi
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

require_tool() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required tool missing: $1" >&2
    exit 2
  fi
}

run_or_print() {
  if [ "$DRY_RUN" -eq 1 ]; then
    if [ "${1:-}" = "tmux" ] && [ "${2:-}" = "set-environment" ] && [ "${5:-}" = "MINIMAX_API_KEYS" ]; then
      log "DRY_RUN tmux set-environment target=${4:-unknown} MINIMAX_API_KEYS key_count=$(minimax_key_count)"
      return 0
    fi
    log "DRY_RUN $*"
  else
    "$@"
  fi
}

http_get() {
  curl -fsS --max-time 8 "$1"
}

load_minimax_env() {
  if [ -n "${MINIMAX_API_KEYS:-}" ]; then
    return 0
  fi
  # Prefer an explicit ~/.chatlog-ha-keys file with chmod 600 to avoid the
  # shell-snapshot interaction that strips MINIMAX_API_KEYS in `zsh -lc`.
  local keyfile="${CHATLOG_HA_KEYS_FILE:-$HOME/.chatlog-ha-keys}"
  if [ -r "$keyfile" ]; then
    local file_keys
    file_keys="$(head -n 1 "$keyfile" 2>/dev/null || true)"
    if [ -n "$file_keys" ]; then
      export MINIMAX_API_KEYS="$file_keys"
      return 0
    fi
  fi
  if command -v zsh >/dev/null 2>&1; then
    local keys
    keys="$(zsh -lc 'printf %s "${MINIMAX_API_KEYS:-}"' 2>/dev/null || true)"
    if [ -n "$keys" ]; then
      export MINIMAX_API_KEYS="$keys"
      return 0
    fi
    # Fall back to a non-interactive `zsh -c` that explicitly sources
    # ~/.zshenv (login shell snapshots may not export the variable).
    if [ -r "$HOME/.zshenv" ]; then
      keys="$(zsh -c 'source "$HOME/.zshenv" >/dev/null 2>&1; printf %s "${MINIMAX_API_KEYS:-}"' 2>/dev/null || true)"
      if [ -n "$keys" ]; then
        export MINIMAX_API_KEYS="$keys"
        return 0
      fi
    fi
    if [ -r "$HOME/.zshrc" ]; then
      keys="$(zsh -c 'source "$HOME/.zshrc" >/dev/null 2>&1; printf %s "${MINIMAX_API_KEYS:-}"' 2>/dev/null || true)"
      if [ -n "$keys" ]; then
        export MINIMAX_API_KEYS="$keys"
        return 0
      fi
    fi
  fi
  return 1
}

minimax_key_count() {
  local raw="${MINIMAX_API_KEYS:-}"
  if [ -z "$raw" ]; then
    echo 0
    return 0
  fi
  awk -v raw="$raw" 'BEGIN { n=split(raw,a,","); c=0; for (i=1;i<=n;i++) if (length(a[i])>0) c++; print c }'
}

# runtime_minimax_status prefers the privacy-safe HTTP endpoint exposed by
# the new chatlog binary, and falls back to env counting so the guard can
# still report key counts when the new endpoint is unavailable.
runtime_minimax_status() {
  local status
  status="$(http_get "$ADDR/api/v1/semantic/mmx/status?format=json" 2>/dev/null || true)"
  if [ -n "$status" ] && [ "$(jq -r '.configured_key_count // empty' <<<"$status" 2>/dev/null)" != "" ]; then
    jq -c '{configured_key_count: .configured_key_count, busy_key_count: (.busy_key_count // 0), idle_key_count: (.idle_key_count // 0), leased_request_count: (.leased_request_count // 0), retry_count: (.retry_count // 0), last_error_bucket: (.last_error_bucket // "")}' <<<"$status"
    return 0
  fi
  jq -nc --argjson c "$(minimax_key_count)" '{configured_key_count: $c, busy_key_count: 0, idle_key_count: $c, leased_request_count: 0, retry_count: 0, last_error_bucket: ""}'
}

sync_tmux_env() {
  local count
  count="$(minimax_key_count)"
  log "minimax_env key_count=$count"
  if [ "$count" -le 0 ]; then
    return 0
  fi
  if tmux has-session -t "$TMUX_SESSION" 2>/dev/null; then
    run_or_print tmux set-environment -t "$TMUX_SESSION" MINIMAX_API_KEYS "$MINIMAX_API_KEYS"
    if [ -n "${MINIMAX_BASE_URL:-}" ]; then
      run_or_print tmux set-environment -t "$TMUX_SESSION" MINIMAX_BASE_URL "$MINIMAX_BASE_URL"
    fi
  fi
}

restart_service() {
  plan_action "restart_service target=$TMUX_TARGET config_dir=$CONFIG_DIR"
  sync_tmux_env
  if [ "$DRY_RUN" -eq 1 ]; then
    log "DRY_RUN tmux respawn/create with serve --config-dir"
    return 0
  fi

  if tmux has-session -t "$TMUX_SESSION" 2>/dev/null; then
    tmux respawn-pane -k -t "$TMUX_TARGET" "cd '$ROOT_DIR' && ./bin/chatlog serve --config-dir '$CONFIG_DIR'"
  else
    tmux new-session -d -s "$TMUX_SESSION" "cd '$ROOT_DIR' && ./bin/chatlog serve --config-dir '$CONFIG_DIR'"
  fi

  for _ in 1 2 3 4 5 6 7 8 9 10; do
    if http_get "$ADDR/health" >/dev/null 2>&1; then
      log "OK health_after_restart"
      return 0
    fi
    sleep 2
  done

  log "ERROR health_after_restart_failed"
  return 1
}

semantic_ready() {
  local cfg provider model has_key
  cfg="$(http_get "$ADDR/api/v1/semantic/config?format=json" 2>/dev/null || true)"
  if [ -z "$cfg" ]; then
    log "WARN semantic_config_unavailable"
    SEMANTIC_PROVIDER=""
    SEMANTIC_MODEL=""
    SEMANTIC_HAS_KEY=""
    return 1
  fi

  provider="$(jq -r '.chat_provider // ""' <<<"$cfg")"
  model="$(jq -r '.chat_model // ""' <<<"$cfg")"
  has_key="$(jq -r '.has_api_key // false' <<<"$cfg")"
  log "semantic provider=$provider model=$model has_api_key=$has_key"
  SEMANTIC_PROVIDER="$provider"
  SEMANTIC_MODEL="$model"
  SEMANTIC_HAS_KEY="$has_key"

  [ "$provider" = "$EXPECTED_PROVIDER" ] && [ "$model" = "$EXPECTED_MODEL" ] && [ "$has_key" = "true" ]
}

# restore_semantic_config POSTs the expected provider/model back into the
# running service so the operator does not have to re-issue a config write
# by hand. The MiniMax key pool remains env-driven; the api_key is left
# untouched (the service already has it from the loaded service config or
# env). This is the lightweight self-heal path for AC#3.
restore_semantic_config() {
  plan_action "restore_semantic_config provider=$EXPECTED_PROVIDER model=$EXPECTED_MODEL"
  run_or_print curl -fsS --max-time 8 -X POST "$ADDR/api/v1/semantic/config?format=json" \
    -H 'Content-Type: application/json' \
    --data "{\"chat_provider\":\"$EXPECTED_PROVIDER\",\"chat_model\":\"$EXPECTED_MODEL\"}" >/dev/null
}

ensure_graph_config() {
  local status workers enqueue_workers
  status="$(http_get "$ADDR/api/v1/graph/status?format=json")"
  workers="$(jq -r '.workers // 0' <<<"$status")"
  enqueue_workers="$(jq -r '.enqueue_workers // 0' <<<"$status")"
  GRAPH_WORKERS="$workers"
  GRAPH_ENQUEUE_WORKERS="$enqueue_workers"
  if [ "$workers" = "$TARGET_WORKERS" ] && [ "$enqueue_workers" = "$TARGET_ENQUEUE_WORKERS" ]; then
    return 0
  fi
  plan_action "graph_config workers=$TARGET_WORKERS enqueue_workers=$TARGET_ENQUEUE_WORKERS current_workers=$workers current_enqueue_workers=$enqueue_workers"
  run_or_print curl -fsS --max-time 8 -X POST "$ADDR/api/v1/graph/config?format=json" \
    -H 'Content-Type: application/json' \
    --data "{\"workers\":$TARGET_WORKERS,\"enqueue_workers\":$TARGET_ENQUEUE_WORKERS}" >/dev/null
}

recoverable_config_failures() {
  local db_path
  db_path="$(http_get "$ADDR/api/v1/graph/status?format=json" | jq -r '.store_path // ""')"
  if [ -z "$db_path" ] || [ ! -f "$db_path" ] || ! command -v sqlite3 >/dev/null 2>&1; then
    echo 0
    return 0
  fi
  sqlite3 -readonly "$db_path" "SELECT COUNT(1) FROM graph_source_records WHERE status='failed' AND error='chat model is not configured';"
}

resume_graph_if_needed() {
  local status running pending failed last_error config_failed
  status="$(http_get "$ADDR/api/v1/graph/status?format=json")"
  running="$(jq -r '.running // false' <<<"$status")"
  pending="$(jq -r '.pending // 0' <<<"$status")"
  failed="$(jq -r '.failed // 0' <<<"$status")"
  last_error="$(jq -r '.last_error // ""' <<<"$status")"
  GRAPH_PENDING="$pending"
  GRAPH_FAILED="$failed"
  GRAPH_LAST_ERROR="${last_error:-none}"
  config_failed="$(recoverable_config_failures)"
  log "graph running=$running pending=$pending failed=$failed config_failed=$config_failed last_error=${last_error:-none}"

  if [ "$RESUME_GRAPH" -ne 1 ]; then
    return 0
  fi
  if [ "$config_failed" -gt 0 ] || [ "$last_error" = "chat model is not configured" ] || { [ "$running" = "false" ] && [ "$pending" -gt 0 ]; }; then
    plan_action "graph_resume"
    run_or_print curl -fsS --max-time 20 -X POST "$ADDR/api/v1/graph/resume?format=json" >/dev/null
  fi
}

check_service_process() {
  # Verify the live listener is `./bin/chatlog serve --config-dir <dir>` AND
  # the process has at least TARGET_KEY_COUNT MINIMAX_API_KEYS in its env.
  # Any mismatch records a planned respawn so AC#3 is observable.
  local port_pid
  port_pid="$(lsof -nP -iTCP:5030 -sTCP:LISTEN -t 2>/dev/null | head -n 1 || true)"
  if [ -z "$port_pid" ]; then
    plan_action "process_check_failed reason=no_listener_pid"
    return 1
  fi
  local cmdline env_blob key_count
  cmdline="$(ps -p "$port_pid" -ww -o command= 2>/dev/null || true)"
  if [ -z "$cmdline" ]; then
    plan_action "process_check_failed reason=ps_unavailable"
    return 1
  fi
  if ! grep -q "serve --config-dir $CONFIG_DIR" <<<"$cmdline"; then
    plan_action "process_check_failed reason=cmd_mismatch current_cmd=$cmdline expected_substring=serve --config-dir $CONFIG_DIR"
    return 1
  fi
  if ! command -v ps >/dev/null 2>&1; then
    return 0
  fi
  env_blob="$(ps eww -p "$port_pid" 2>/dev/null || true)"
  if ! grep -q 'MINIMAX_API_KEYS' <<<"$env_blob"; then
    plan_action "process_check_failed reason=process_env_has_no_minimax_keys current_cmd=$cmdline"
    return 1
  fi
  key_count="$(awk 'BEGIN { RS="[ \n]" } $0 ~ /^MINIMAX_API_KEYS=/ { sub(/^MINIMAX_API_KEYS=/, ""); n=split($0, a, ","); c=0; for (i=1;i<=n;i++) if (length(a[i])>0) c++; print c; exit }' <<<"$env_blob")"
  key_count="${key_count:-0}"
  if [ "$key_count" -lt "$TARGET_KEY_COUNT" ] 2>/dev/null; then
    plan_action "process_check_failed reason=process_env_key_count=$key_count target=$TARGET_KEY_COUNT current_cmd=$cmdline"
    return 1
  fi
  return 0
}

check_once() {
  cd "$ROOT_DIR"
  require_tool curl
  require_tool jq
  require_tool tmux
  reset_planned
  load_minimax_env
  sync_tmux_env

  GUARD_HEALTH="unknown"
  if http_get "$ADDR/health" >/dev/null 2>&1; then
    GUARD_HEALTH="ok"
  else
    GUARD_HEALTH="down"
  fi
  log "health=$GUARD_HEALTH"

  if [ "$GUARD_HEALTH" != "ok" ]; then
    plan_action "restart_service reason=health_down"
    if [ "$DRY_RUN" -ne 1 ]; then
      restart_service
    fi
  fi

  SEMANTIC_PROVIDER=""
  SEMANTIC_MODEL=""
  SEMANTIC_HAS_KEY=""
  if ! semantic_ready; then
    log "WARN semantic_config_drift expected_provider=$EXPECTED_PROVIDER expected_model=$EXPECTED_MODEL"
    if [ "$DRY_RUN" -ne 1 ]; then
      restore_semantic_config
      if ! semantic_ready; then
        log "WARN semantic_still_drifted_after_post, respawn with config-dir"
        restart_service
        if ! semantic_ready; then
          log "ERROR semantic_config_not_ready_after_restart"
          emit_summary_block "drift" "yes"
          return 1
        fi
      fi
    else
      plan_action "restore_semantic_config provider=$EXPECTED_PROVIDER model=$EXPECTED_MODEL (dry-run)"
    fi
  fi

  # Surface the privacy-safe mmx/status snapshot so the operator can see
  # configured/busy/idle key counts and the last error bucket without
  # exposing real API keys.
  local mmx_status
  mmx_status="$(runtime_minimax_status 2>/dev/null || true)"
  MMX_CONFIGURED="0"
  if [ -n "$mmx_status" ]; then
    MMX_CONFIGURED="$(jq -r '.configured_key_count // 0' <<<"$mmx_status")"
    log "mmx_status configured=$MMX_CONFIGURED busy=$(jq -r '.busy_key_count // 0' <<<"$mmx_status") idle=$(jq -r '.idle_key_count // 0' <<<"$mmx_status") retry=$(jq -r '.retry_count // 0' <<<"$mmx_status") last_error_bucket=$(jq -r '.last_error_bucket // ""' <<<"$mmx_status")"
    if [ "$MMX_CONFIGURED" -lt "$TARGET_KEY_COUNT" ] 2>/dev/null; then
      log "WARN mmx_key_underprovisioned configured=$MMX_CONFIGURED target=$TARGET_KEY_COUNT"
      plan_action "restart_service reason=mmx_key_underprovisioned"
      if [ "$DRY_RUN" -ne 1 ]; then
        restart_service
      fi
    fi
  fi

  # Process-level checks: live cmdline + process env key count.
  # In DRY_RUN mode we only plan, in live mode we respawn via restart_service.
  if ! check_service_process; then
    if [ "$DRY_RUN" -ne 1 ]; then
      restart_service
    fi
  fi

  if [ "$GUARD_HEALTH" = "ok" ]; then
    ensure_graph_config
    resume_graph_if_needed
  fi

  # Bucket summary (privacy-safe counts only, no source payload/prompt).
  local graph_status bucket_json
  graph_status="$(http_get "$ADDR/api/v1/graph/status?format=json" 2>/dev/null || true)"
  if [ -n "$graph_status" ]; then
    bucket_json="$(jq -c '.failed_buckets // {}' <<<"$graph_status" 2>/dev/null || echo "{}")"
    log "failed_buckets=$bucket_json"
  fi

  local action_taken="no"
  if [ "$DRY_RUN" -ne 1 ] && [ "${#PLANNED_ACTIONS[@]}" -gt 0 ]; then
    action_taken="yes"
  fi
  if [ "$LOOP_INTERVAL" -eq 0 ]; then
    emit_summary_block "once" "$action_taken"
  fi
}

if [ "$LOOP_INTERVAL" -gt 0 ]; then
  log "START loop interval=${LOOP_INTERVAL}s"
  while true; do
    reset_planned
    check_once || true
    loop_action_taken="no"
    if [ "$DRY_RUN" -ne 1 ] && [ "${#PLANNED_ACTIONS[@]}" -gt 0 ]; then
      loop_action_taken="yes"
    fi
    emit_summary_block "tick" "$loop_action_taken"
    sleep "$LOOP_INTERVAL"
  done
else
  check_once
fi
