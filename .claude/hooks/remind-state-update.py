#!/usr/bin/env python3
"""remind-state-update: Stop hook enforcing the DoD state-file update.

When the agent tries to stop while it has modified ``*.go`` files but has NOT
touched ``progress.md``, AGENTS.md's Definition of Done is unmet — code changed
with no durable state record. This hook blocks the first Stop with a JSON
``{"decision": "block"}`` so the agent goes back and updates progress.md.

Self-termination (CRITICAL — Stop hooks have no built-in loop guard): we keep a
per-session counter at
``$CLAUDE_PROJECT_DIR/.claude/hooks/.stop_block_count.<sha256(session_id)[:12]>``.
The first block increments the counter to 1. On the second and any later Stop
for the SAME session we emit ``{"continue": false, "stopReason": ...}`` to force
the session to stop, so a stubborn agent can never be trapped in an infinite
block loop.

Output protocol (NOT exit codes): this hook always exits 0 and communicates via
a JSON object on stdout:
- block:    {"decision": "block", "reason": ...}
- stop:     {"continue": false, "stopReason": ...}
- allow:    {}

Safety: not in a git repo / git failing → empty dirty set → allow ({}). Never
crashes the session.
"""

import hashlib
import json
import os
import subprocess
import sys

# Make sibling _common importable regardless of how the harness invokes us.
_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

from _common import read_event  # noqa: E402


def _emit(obj):
    """Write a JSON object to stdout and exit 0 (Stop hook protocol)."""
    sys.stdout.write(json.dumps(obj))
    sys.exit(0)


def _dirty_set():
    """Return the set of paths from ``git status --porcelain``.

    Empty set when not in a git repo or when git fails, so the hook never
    crashes and simply allows the stop.
    """
    cwd = os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd()
    try:
        proc = subprocess.run(
            ["git", "status", "--porcelain"],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            cwd=cwd,
            timeout=5,
        )
    except Exception:
        return set()
    if proc.returncode != 0:
        return set()
    dirty = set()
    for line in proc.stdout.decode("utf-8", "replace").splitlines():
        # Porcelain v1: "XY PATH"; renames are "R  old -> new".
        if len(line) < 4:
            continue
        path = line[3:].strip()
        if " -> " in path:
            path = path.split(" -> ", 1)[1]
        path = path.strip().strip('"')
        if path:
            dirty.add(path)
    return dirty


def _has_go(dirty):
    return any(p.endswith(".go") for p in dirty)


def _has_progress(dirty):
    return any(p == "progress.md" or p.endswith("/progress.md") for p in dirty)


def _counter_path(session_id):
    digest = hashlib.sha256(str(session_id).encode("utf-8")).hexdigest()[:12]
    return os.path.join(_HERE, ".stop_block_count." + digest)


def _read_count(path):
    try:
        with open(path, "r") as fh:
            return int((fh.read() or "0").strip() or "0")
    except Exception:
        return 0


def _write_count(path, count):
    try:
        with open(path, "w") as fh:
            fh.write(str(count))
    except Exception:
        # Counter persistence is best-effort; never crash the session.
        pass


def main():
    event = read_event() or {}

    # Only act on Stop events; anything else allows the stop.
    name = event.get("hook_event_name")
    if name and name != "Stop":
        return _emit({})

    dirty = _dirty_set()
    # DoD violation only when Go code changed but progress.md was not updated.
    if not (_has_go(dirty) and not _has_progress(dirty)):
        return _emit({})

    session_id = event.get("session_id") or ""
    path = _counter_path(session_id)
    count = _read_count(path) + 1
    _write_count(path, count)

    if count >= 2:
        # Second+ block for this session → force stop to avoid an infinite loop.
        return _emit({
            "continue": False,
            "stopReason": (
                "Stopping: progress.md still not updated after a prior reminder. "
                "Per AGENTS.md Definition of Done, update progress.md to record "
                "what changed before finishing."
            ),
        })

    # First block → ask the agent to update progress.md.
    return _emit({
        "decision": "block",
        "reason": (
            "You modified .go files but did not update progress.md. Per "
            "AGENTS.md Definition of Done, record What changed / Verification "
            "Evidence / Risks / Next in progress.md before stopping."
        ),
    })


if __name__ == "__main__":
    main()
