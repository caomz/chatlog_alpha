#!/usr/bin/env python3
"""block-destructive-git: PreToolUse guard for Bash.

Reads tool_input.command from stdin and blocks *destructive* git operations
that AGENTS.md reserves for the user / for scripts/ralph/ralph.py:

- ``git commit`` / ``git push`` / ``git reset`` / ``git rebase`` (history /
  publish / working-tree rewrites).
- ``git merge`` (branch lifecycle is owned by ralph.py) — except the recovery
  forms ``--abort`` / ``--continue`` / ``--quit``, which un-stick a half-done
  merge and are allowed.
- ``git checkout -b`` / ``git checkout -B`` (branch creation / switch).
- ``git checkout`` / ``git restore`` / ``git reset`` targeting a path that is in
  the current dirty set (dirty-revert protection: do not clobber files the user
  or another run left modified).

Read-only / introspection commands stay allowed so automation keeps working:
``git status`` / ``git diff`` / ``git log`` / ``git rev-parse --abbrev-ref HEAD``
(the last is used by ralph.py and MUST pass).

Safety: if we are not inside a git repo or ``git status`` fails, we fall back to
an empty dirty set — the command-name rules still apply and the hook never
crashes.
"""

import os
import re
import subprocess
import sys

# Make sibling _common importable regardless of how the harness invokes us.
_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

from _common import allow, deny, read_event, rel_to_project  # noqa: E402

# git subcommands that are always destructive regardless of arguments.
_ALWAYS_BLOCK = {
    "commit": "git commit (commits are made by scripts/ralph/ralph.py after validation)",
    "push": "git push (publishing is never done automatically)",
    "reset": "git reset (rewrites index / working tree)",
    "rebase": "git rebase (rewrites history)",
}
# merge recovery flags that un-stick a half-done merge — allowed.
_MERGE_RECOVERY = {"--abort", "--continue", "--quit"}
# subcommands whose path arguments are checked against the dirty set.
_DIRTY_REVERT_SUBS = {"checkout", "restore", "reset"}


def _segments(cmd):
    """Split a command line into pipeline / sequence segments."""
    return [s for s in re.split(r"[|;&\n()]+", cmd) if s.strip()]


def _tokens_after_name(seg):
    """Return (name, args) for a segment, skipping leading VAR=value prefixes."""
    tokens = seg.strip().split()
    i = 0
    while i < len(tokens) and "=" in tokens[i] and not tokens[i].startswith("-"):
        i += 1
    if i >= len(tokens):
        return None, []
    name = tokens[i].rsplit("/", 1)[-1]
    return name, tokens[i + 1:]


def _dirty_set():
    """Return the set of paths reported by ``git status --porcelain``.

    Returns an empty set when not in a git repo or when git fails, so the
    command-name rules still apply and the hook never crashes.
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


def _path_args(args):
    """Return non-flag path tokens (quotes stripped, ``--`` separator dropped)."""
    paths = []
    for tok in args:
        if tok == "--":
            continue
        if tok.startswith("-"):
            continue
        paths.append(tok.strip("'\""))
    return paths


def _check_git(args, dirty):
    """Return a deny reason for a destructive git invocation, else None."""
    if not args:
        return None
    sub = args[0]
    rest = args[1:]

    if sub in _ALWAYS_BLOCK and sub not in _DIRTY_REVERT_SUBS:
        return _ALWAYS_BLOCK[sub]

    if sub == "merge":
        if any(flag in rest for flag in _MERGE_RECOVERY):
            return None
        return "git merge (branch lifecycle is owned by scripts/ralph/ralph.py)"

    if sub == "checkout" and any(f in ("-b", "-B") for f in rest):
        return "git checkout -b / -B (branch creation / switch)"

    # dirty-revert protection for checkout / restore / reset.
    if sub in _DIRTY_REVERT_SUBS:
        for p in _path_args(rest):
            if rel_to_project(p) in dirty:
                return "dirty-revert of '{}' (would clobber a modified file)".format(p)
        # reset with no dirty path still rewrites index/working tree.
        if sub == "reset":
            return _ALWAYS_BLOCK["reset"]

    return None


def main():
    event = read_event()
    tool_input = (event or {}).get("tool_input") or {}
    cmd = tool_input.get("command")
    if not cmd or not cmd.strip():
        return allow()

    dirty = None
    for seg in _segments(cmd):
        name, args = _tokens_after_name(seg)
        if name != "git":
            continue
        if dirty is None:
            dirty = _dirty_set()
        reason = _check_git(args, dirty)
        if reason:
            return deny(
                "Blocked: refusing destructive git operation ({}). Branch and "
                "history operations are reserved for the user / "
                "scripts/ralph/ralph.py.".format(reason)
            )

    return allow()


if __name__ == "__main__":
    main()
