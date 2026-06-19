#!/usr/bin/env python3
"""block-batch-delete: PreToolUse guard for Bash.

Reads tool_input.command from stdin and blocks *batch* / recursive deletion
commands. AGENTS.md forbids scripted batch deletion: "Do not use scripts to
batch-delete files or directories. If deletion is unavoidable, delete one file
at a time and explain risk/rollback first."

Blocked patterns (any pipeline / sequence segment):

- ``rm`` with a recursive flag (-r / -R / -rf / -fr / --recursive).
- ``rm`` with a glob / wildcard in a path argument (``*`` / ``?`` / ``[``).
- ``rm`` with more than one path argument (batch of explicit files).
- ``find ... -delete`` or ``find ... -exec rm`` (and rmdir / unlink).
- ``git clean`` (prunes untracked / ignored files in bulk).

Single-file deletes (``rm one.txt`` / ``rm -f one.txt``) and read-only commands
(``ls -la``) are allowed.
"""

import os
import re
import sys

# Make sibling _common importable regardless of how the harness invokes us.
_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

from _common import allow, deny, read_event  # noqa: E402

# Commands that perform a deletion when invoked under find -exec.
_DELETE_EXEC = {"rm", "rmdir", "unlink"}
# Glob / wildcard characters that turn a single rm into a batch operation.
_GLOB_CHARS = ("*", "?", "[")


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


def _is_recursive_flag(tok):
    """True for an rm recursive flag (-r / -R / -rf / -fr / --recursive)."""
    if tok.startswith("--"):
        return tok == "--recursive"
    if tok.startswith("-"):
        return "r" in tok or "R" in tok
    return False


def _basename(tok):
    return tok.strip("'\"").rsplit("/", 1)[-1]


def _check_rm(args):
    """Return a deny reason string if this rm invocation is a batch delete."""
    paths = []
    for tok in args:
        if tok.startswith("-"):
            if _is_recursive_flag(tok):
                return "recursive rm (-r/-R)"
            continue
        bare = tok.strip("'\"")
        if any(g in bare for g in _GLOB_CHARS):
            return "rm with a glob / wildcard ('{}')".format(bare)
        paths.append(bare)
    if len(paths) > 1:
        return "rm of multiple paths in one command"
    return None


def _check_find(args):
    """Return a deny reason if this find deletes files (-delete / -exec rm)."""
    if "-delete" in args:
        return "find ... -delete"
    if "-exec" in args or "-execdir" in args:
        for tok in args:
            if _basename(tok) in _DELETE_EXEC:
                return "find ... -exec rm"
    return None


def main():
    event = read_event()
    tool_input = (event or {}).get("tool_input") or {}
    cmd = tool_input.get("command")
    if not cmd or not cmd.strip():
        return allow()

    for seg in _segments(cmd):
        name, args = _tokens_after_name(seg)
        if name is None:
            continue
        reason = None
        if name == "rm":
            reason = _check_rm(args)
        elif name == "find":
            reason = _check_find(args)
        elif name == "git" and args and args[0] == "clean":
            reason = "git clean (bulk prune of untracked files)"
        if reason:
            return deny(
                "Blocked: refusing batch deletion ({}). AGENTS.md requires "
                "deleting one file at a time and explaining risk / rollback "
                "first.".format(reason)
            )

    return allow()


if __name__ == "__main__":
    main()
