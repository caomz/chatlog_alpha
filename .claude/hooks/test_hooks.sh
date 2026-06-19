#!/usr/bin/env bash
# test_hooks.sh — offline standalone test harness for the chatlog_alpha
# Claude Code guard hooks (US-001..US-009).
#
# Runs every hook as a subprocess with crafted stdin JSON and asserts the
# exit code (PreToolUse / PostToolUse hooks) or the stdout JSON (Stop hook),
# WITHOUT needing a live Claude Code session. Run before wiring the hooks into
# .claude/settings.json to confirm behaviour is correct.
#
# Usage:  bash .claude/hooks/test_hooks.sh
# Exit:   0 + "ALL HOOK TESTS PASSED" when every assertion passes; 1 otherwise.

set -u

# --- locate repo root (this script lives in <root>/.claude/hooks/) -----------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd -P)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd -P)"
HOOKS="$SCRIPT_DIR"
cd "$REPO_ROOT"

# Anchor private-path / dirty-set checks to the real repo root.
export CLAUDE_PROJECT_DIR="$REPO_ROOT"

# --- counters ----------------------------------------------------------------
fail=0
total=0

# --- side-file cleanup (Stop hook counters) ---------------------------------
cleanup_counters() {
  rm -f "$HOOKS"/.stop_block_count.* 2>/dev/null || true
}
cleanup_counters   # start clean

GO_TMPS=""
cleanup_all() {
  cleanup_counters
  # shellcheck disable=SC2086
  [ -n "$GO_TMPS" ] && rm -f $GO_TMPS 2>/dev/null || true
}
trap cleanup_all EXIT

# --- assertion helpers -------------------------------------------------------
# check_code <expected> <label> <actual>  (uses the required actual=$? form)
check_code() {
  expected="$1"; label="$2"; actual="$3"
  total=$((total + 1))
  [ "$actual" = "$expected" ] || { echo "FAIL: $label (expected exit $expected, got $actual)"; fail=1; }
}

# check_contains <needle> <label> <haystack>
check_contains() {
  needle="$1"; label="$2"; haystack="$3"
  total=$((total + 1))
  case "$haystack" in
    *"$needle"*) : ;;
    *) echo "FAIL: $label (output missing [$needle]; got: $haystack)"; fail=1 ;;
  esac
}

# run_hook <hook-file> <json-stdin>  -> echoes stdout, sets global RC
run_hook() {
  RC_OUT="$(printf '%s' "$2" | python3 "$HOOKS/$1")"
  RC=$?
}

# py_compile assertion
compiles() {
  python3 -m py_compile "$HOOKS/$1"; actual=$?
  check_code 0 "py_compile $1" "$actual"
}

echo "== chatlog_alpha guard hook offline tests =="
echo "repo: $REPO_ROOT"

# =============================================================================
# US-001  _common.py (is_private semantics + compile)
# =============================================================================
compiles _common.py

python3 -c 'import sys; sys.path.insert(0,sys.argv[1]); import _common as c; sys.exit(0 if c.is_private("reports/x.md") else 1)' "$HOOKS"; actual=$?
check_code 0 "US-001 is_private(reports/x.md)=True" "$actual"
python3 -c 'import sys; sys.path.insert(0,sys.argv[1]); import _common as c; sys.exit(0 if c.is_private(".env") else 1)' "$HOOKS"; actual=$?
check_code 0 "US-001 is_private(.env)=True" "$actual"
python3 -c 'import sys; sys.path.insert(0,sys.argv[1]); import _common as c; sys.exit(0 if c.is_private("logs/x.log") else 1)' "$HOOKS"; actual=$?
check_code 0 "US-001 is_private(logs/x.log)=True" "$actual"
python3 -c 'import sys; sys.path.insert(0,sys.argv[1]); import _common as c; sys.exit(1 if c.is_private(".env.example") else 0)' "$HOOKS"; actual=$?
check_code 0 "US-001 is_private(.env.example)=False" "$actual"
python3 -c 'import sys; sys.path.insert(0,sys.argv[1]); import _common as c; sys.exit(1 if c.is_private("internal/chatlog/x.go") else 0)' "$HOOKS"; actual=$?
check_code 0 "US-001 is_private(internal/chatlog/x.go)=False" "$actual"

# =============================================================================
# US-002  block-private-writes.py  (PreToolUse / Write|Edit|MultiEdit, file_path)
# =============================================================================
compiles block-private-writes.py
run_hook block-private-writes.py '{"tool_name":"Write","tool_input":{"file_path":"reports/a.md"}}';        check_code 2 "US-002 write reports/a.md" "$RC"
run_hook block-private-writes.py '{"tool_name":"Write","tool_input":{"file_path":"internal/chatlog/x.go"}}'; check_code 0 "US-002 write internal/chatlog/x.go" "$RC"
run_hook block-private-writes.py '{"tool_name":"Write","tool_input":{"file_path":".env.example"}}';         check_code 0 "US-002 write .env.example" "$RC"
run_hook block-private-writes.py '{"tool_name":"Write","tool_input":{"file_path":".claude/hooks/block-private-writes.py"}}'; check_code 0 "US-002 self-modify hook" "$RC"
run_hook block-private-writes.py '{"tool_name":"Write","tool_input":{"file_path":".claude/settings.json"}}'; check_code 0 "US-002 self-config settings.json" "$RC"
run_hook block-private-writes.py '{"tool_name":"Write","tool_input":{}}';                                   check_code 0 "US-002 missing file_path" "$RC"
run_hook block-private-writes.py '{"tool_name":"Write","tool_input":{"file_path":".env.production"}}';      check_code 2 "US-002 write .env.production" "$RC"

# =============================================================================
# US-003  block-private-reads.py  (PreToolUse / Bash, command)
# =============================================================================
compiles block-private-reads.py
run_hook block-private-reads.py '{"tool_name":"Bash","tool_input":{"command":"cat reports/daily.md"}}'; check_code 2 "US-003 cat reports/daily.md" "$RC"
run_hook block-private-reads.py '{"tool_name":"Bash","tool_input":{"command":"head -n 5 .env"}}';       check_code 2 "US-003 head .env" "$RC"
run_hook block-private-reads.py '{"tool_name":"Bash","tool_input":{"command":"tail -f logs/x.log"}}';   check_code 2 "US-003 tail logs/x.log" "$RC"
run_hook block-private-reads.py '{"tool_name":"Bash","tool_input":{"command":"grep error logs/x.log"}}';check_code 2 "US-003 grep logs/x.log" "$RC"
run_hook block-private-reads.py '{"tool_name":"Bash","tool_input":{"command":"jq .x reports/y.json"}}';  check_code 2 "US-003 jq reports/y.json" "$RC"
run_hook block-private-reads.py '{"tool_name":"Bash","tool_input":{"command":"ls -la reports/"}}';       check_code 0 "US-003 ls reports/" "$RC"
run_hook block-private-reads.py '{"tool_name":"Bash","tool_input":{"command":"wc -l reports/x.md"}}';    check_code 0 "US-003 wc reports/x.md" "$RC"
run_hook block-private-reads.py '{"tool_name":"Bash","tool_input":{"command":"stat .env"}}';             check_code 0 "US-003 stat .env" "$RC"
run_hook block-private-reads.py '{"tool_name":"Bash","tool_input":{"command":"cat internal/chatlog/x.go"}}'; check_code 0 "US-003 cat internal/chatlog/x.go" "$RC"

# =============================================================================
# US-004  block-batch-delete.py  (PreToolUse / Bash, command)
# =============================================================================
compiles block-batch-delete.py
run_hook block-batch-delete.py '{"tool_name":"Bash","tool_input":{"command":"rm -rf build/"}}';   check_code 2 "US-004 rm -rf build/" "$RC"
run_hook block-batch-delete.py '{"tool_name":"Bash","tool_input":{"command":"rm -fr /tmp/foo"}}';  check_code 2 "US-004 rm -fr /tmp/foo" "$RC"
run_hook block-batch-delete.py '{"tool_name":"Bash","tool_input":{"command":"find . -name \"*.tmp\" -delete"}}'; check_code 2 "US-004 find -delete" "$RC"
run_hook block-batch-delete.py '{"tool_name":"Bash","tool_input":{"command":"find . -name \"*.log\" -exec rm {} ;"}}'; check_code 2 "US-004 find -exec rm" "$RC"
run_hook block-batch-delete.py '{"tool_name":"Bash","tool_input":{"command":"git clean -fd"}}';   check_code 2 "US-004 git clean -fd" "$RC"
run_hook block-batch-delete.py '{"tool_name":"Bash","tool_input":{"command":"rm logs/*.log"}}';   check_code 2 "US-004 rm logs/*.log" "$RC"
run_hook block-batch-delete.py '{"tool_name":"Bash","tool_input":{"command":"rm -f one.txt"}}';   check_code 0 "US-004 rm -f one.txt" "$RC"
run_hook block-batch-delete.py '{"tool_name":"Bash","tool_input":{"command":"rm one.txt"}}';      check_code 0 "US-004 rm one.txt" "$RC"
run_hook block-batch-delete.py '{"tool_name":"Bash","tool_input":{"command":"ls -la"}}';          check_code 0 "US-004 ls -la" "$RC"

# =============================================================================
# US-005  block-destructive-git.py  (PreToolUse / Bash, command + dirty-revert)
# =============================================================================
compiles block-destructive-git.py
run_hook block-destructive-git.py '{"tool_name":"Bash","tool_input":{"command":"git commit -m x"}}';        check_code 2 "US-005 git commit" "$RC"
run_hook block-destructive-git.py '{"tool_name":"Bash","tool_input":{"command":"git push origin main"}}';   check_code 2 "US-005 git push" "$RC"
run_hook block-destructive-git.py '{"tool_name":"Bash","tool_input":{"command":"git reset --hard HEAD~1"}}';check_code 2 "US-005 git reset --hard" "$RC"
run_hook block-destructive-git.py '{"tool_name":"Bash","tool_input":{"command":"git rebase -i HEAD~3"}}';   check_code 2 "US-005 git rebase" "$RC"
run_hook block-destructive-git.py '{"tool_name":"Bash","tool_input":{"command":"git merge --no-ff feature"}}'; check_code 2 "US-005 git merge --no-ff" "$RC"
run_hook block-destructive-git.py '{"tool_name":"Bash","tool_input":{"command":"git merge --abort"}}';      check_code 0 "US-005 git merge --abort" "$RC"
run_hook block-destructive-git.py '{"tool_name":"Bash","tool_input":{"command":"git checkout -b foo"}}';    check_code 2 "US-005 git checkout -b" "$RC"
run_hook block-destructive-git.py '{"tool_name":"Bash","tool_input":{"command":"git status"}}';             check_code 0 "US-005 git status" "$RC"
run_hook block-destructive-git.py '{"tool_name":"Bash","tool_input":{"command":"git rev-parse --abbrev-ref HEAD"}}'; check_code 0 "US-005 git rev-parse" "$RC"
run_hook block-destructive-git.py '{"tool_name":"Bash","tool_input":{"command":"git log --oneline -5"}}';   check_code 0 "US-005 git log" "$RC"
run_hook block-destructive-git.py '{"tool_name":"Bash","tool_input":{"command":"git diff"}}';               check_code 0 "US-005 git diff" "$RC"

# dirty-revert: run at repo root against a currently-dirty tracked file.
DIRTY_FILE="$(git status --porcelain 2>/dev/null | awk 'substr($0,1,2) ~ /M/ {print substr($0,4); exit}')"
[ -n "$DIRTY_FILE" ] || DIRTY_FILE="CLAUDE.md"
run_hook block-destructive-git.py "{\"tool_name\":\"Bash\",\"tool_input\":{\"command\":\"git restore $DIRTY_FILE\"}}"; check_code 2 "US-005 dirty-revert git restore $DIRTY_FILE" "$RC"

# =============================================================================
# US-006  block-report-commit.py  (PreToolUse / Bash, command)
# =============================================================================
compiles block-report-commit.py
run_hook block-report-commit.py '{"tool_name":"Bash","tool_input":{"command":"git add ."}}';      check_code 2 "US-006 git add ." "$RC"
run_hook block-report-commit.py '{"tool_name":"Bash","tool_input":{"command":"git add -A"}}';     check_code 2 "US-006 git add -A" "$RC"
run_hook block-report-commit.py '{"tool_name":"Bash","tool_input":{"command":"git add --all"}}';  check_code 2 "US-006 git add --all" "$RC"
run_hook block-report-commit.py '{"tool_name":"Bash","tool_input":{"command":"git add reports/x.md"}}'; check_code 2 "US-006 git add reports/x.md" "$RC"
run_hook block-report-commit.py '{"tool_name":"Bash","tool_input":{"command":"git add internal/chatlog/x.go"}}'; check_code 0 "US-006 git add internal/...go" "$RC"
run_hook block-report-commit.py '{"tool_name":"Bash","tool_input":{"command":"git status"}}';     check_code 0 "US-006 git status" "$RC"
run_hook block-report-commit.py '{"tool_name":"Bash","tool_input":{"command":"git diff --cached"}}'; check_code 0 "US-006 git diff --cached" "$RC"

# =============================================================================
# US-007  guard-quota-commands.py  (PreToolUse / Bash, command)
# =============================================================================
compiles guard-quota-commands.py
run_hook guard-quota-commands.py '{"tool_name":"Bash","tool_input":{"command":"chatlog report daily --vision"}}';  check_code 2 "US-007 report daily --vision" "$RC"
run_hook guard-quota-commands.py '{"tool_name":"Bash","tool_input":{"command":"chatlog report daily --summary"}}'; check_code 2 "US-007 report daily --summary" "$RC"
run_hook guard-quota-commands.py '{"tool_name":"Bash","tool_input":{"command":"curl -s http://127.0.0.1:5030/api/v1/semantic/test"}}'; check_code 2 "US-007 curl semantic/test" "$RC"
run_hook guard-quota-commands.py '{"tool_name":"Bash","tool_input":{"command":"chatlog semantic test"}}';          check_code 2 "US-007 chatlog semantic test" "$RC"
run_hook guard-quota-commands.py '{"tool_name":"Bash","tool_input":{"command":"go run . report daily --help"}}';   check_code 0 "US-007 report daily --help" "$RC"
run_hook guard-quota-commands.py '{"tool_name":"Bash","tool_input":{"command":"chatlog report daily"}}';           check_code 0 "US-007 report daily (plain)" "$RC"
run_hook guard-quota-commands.py '{"tool_name":"Bash","tool_input":{"command":"chatlog http list"}}';              check_code 0 "US-007 chatlog http list" "$RC"

# =============================================================================
# US-008  gofmt-check.py  (PostToolUse / Write|Edit|MultiEdit, file_path)
# =============================================================================
compiles gofmt-check.py
if command -v gofmt >/dev/null 2>&1; then
  UNFMT="/tmp/_chatlog_hooktest_unfmt.go"
  FMT="/tmp/_chatlog_hooktest_fmt.go"
  GO_TMPS="$UNFMT $FMT"
  printf 'package x\nfunc F( ){\n}\n' > "$UNFMT"
  printf 'package x\n' > "$FMT"

  ERRFILE="$(mktemp)"
  printf '%s' "{\"tool_name\":\"Write\",\"tool_input\":{\"file_path\":\"$UNFMT\"}}" | python3 "$HOOKS/gofmt-check.py" 2>"$ERRFILE" 1>/dev/null; actual=$?
  ERR="$(cat "$ERRFILE")"; rm -f "$ERRFILE"
  check_code 2 "US-008 unformatted .go" "$actual"
  check_contains "Reminder" "US-008 unformatted .go stderr Reminder" "$ERR"
  check_contains "gofmt -w" "US-008 unformatted .go stderr gofmt -w" "$ERR"

  run_hook gofmt-check.py "{\"tool_name\":\"Write\",\"tool_input\":{\"file_path\":\"$FMT\"}}"; check_code 0 "US-008 formatted .go" "$RC"
else
  echo "WARN: gofmt not in PATH — skipping US-008 gofmt assertions (FileNotFoundError path)"
fi
run_hook gofmt-check.py '{"tool_name":"Write","tool_input":{"file_path":"README.md"}}'; check_code 0 "US-008 non-.go README.md" "$RC"

# =============================================================================
# US-009  remind-state-update.py  (Stop hook, stdout JSON + self-terminate)
# Uses a throwaway git repo so the dirty set is fully controlled. Counter
# files land in the real .claude/hooks/ and are cleaned at start/end.
# =============================================================================
compiles remind-state-update.py
cleanup_counters
STOP_REPO="$(mktemp -d -t chatlog-hooks-stop-XXXXXX)"
(
  cd "$STOP_REPO" && git init -q && git config user.email t@t && git config user.name t \
    && echo "package x" > foo.go
) >/dev/null 2>&1
SID="session-US010-test"

# First Stop: dirty .go + no progress.md → block
RC_OUT="$(printf '%s' "{\"hook_event_name\":\"Stop\",\"session_id\":\"$SID\"}" | CLAUDE_PROJECT_DIR="$STOP_REPO" python3 "$HOOKS/remind-state-update.py")"
check_contains '"decision": "block"' "US-009 first Stop blocks" "$RC_OUT"

# Second Stop, same session → continue:false (self-terminate)
RC_OUT="$(printf '%s' "{\"hook_event_name\":\"Stop\",\"session_id\":\"$SID\"}" | CLAUDE_PROJECT_DIR="$STOP_REPO" python3 "$HOOKS/remind-state-update.py")"
check_contains '"continue": false' "US-009 second Stop self-terminates" "$RC_OUT"

# Third Stop, same session → still continue:false
RC_OUT="$(printf '%s' "{\"hook_event_name\":\"Stop\",\"session_id\":\"$SID\"}" | CLAUDE_PROJECT_DIR="$STOP_REPO" python3 "$HOOKS/remind-state-update.py")"
check_contains '"continue": false' "US-009 third Stop self-terminates" "$RC_OUT"

# dirty set contains progress.md → allow ({})
(cd "$STOP_REPO" && echo "x" > progress.md) >/dev/null 2>&1
RC_OUT="$(printf '%s' "{\"hook_event_name\":\"Stop\",\"session_id\":\"$SID-b\"}" | CLAUDE_PROJECT_DIR="$STOP_REPO" python3 "$HOOKS/remind-state-update.py")"
check_code 0 "US-009 progress.md present allows (exit)" "$?"
check_contains '{}' "US-009 progress.md present allows ({})" "$RC_OUT"

# dirty set has no .go → allow ({})
STOP_REPO2="$(mktemp -d -t chatlog-hooks-stop2-XXXXXX)"
(
  cd "$STOP_REPO2" && git init -q && git config user.email t@t && git config user.name t \
    && echo "x" > README.md
) >/dev/null 2>&1
RC_OUT="$(printf '%s' "{\"hook_event_name\":\"Stop\",\"session_id\":\"$SID-c\"}" | CLAUDE_PROJECT_DIR="$STOP_REPO2" python3 "$HOOKS/remind-state-update.py")"
check_contains '{}' "US-009 no .go allows ({})" "$RC_OUT"

rm -rf "$STOP_REPO" "$STOP_REPO2" 2>/dev/null || true
cleanup_counters

# =============================================================================
# Summary
# =============================================================================
echo "----------------------------------------"
echo "assertions run: $total"
if [ "$fail" = "0" ]; then
  echo "ALL HOOK TESTS PASSED"
  exit 0
else
  echo "SOME HOOK TESTS FAILED"
  exit 1
fi
