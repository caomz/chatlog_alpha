#!/usr/bin/env python3
"""block-report-commit: PreToolUse guard for Bash.

Reads tool_input.command from stdin and blocks ``git add`` invocations that
would sweep private / generated artifacts into the index:

- Bulk staging — ``git add .`` / ``git add -A`` / ``git add --all`` / ``git add -u``
  — stages everything in one shot, so reports/, .cache/, logs/, outputs/, .env*
  etc. ride along. AGENTS.md forbids committing those, so bulk staging is denied.
- ``git add <private path>`` — an explicit private target (e.g. ``reports/x.md``)
  is denied via the same is_private() rule the write/read hooks use.

Explicit project-code paths (``git add internal/chatlog/x.go``) are allowed, and
read-only git introspection (``git status`` / ``git diff --cached``) always
passes so automation keeps working.
"""

import os
import re
import sys

# Make sibling _common importable regardless of how the harness invokes us.
_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

from _common import allow, deny, is_private, read_event  # noqa: E402

# Flags / pathspecs that stage the whole working tree in one shot.
_BULK_FLAGS = {"-A", "--all", "-u", "--update"}
_BULK_PATHSPECS = {".", "./", "*"}


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


def _check_add(args):
    """Return a deny reason for an unsafe ``git add``, else None."""
    if not args or args[0] != "add":
        return None
    rest = args[1:]

    path_seen = False
    for tok in rest:
        if tok == "--":
            continue
        if tok in _BULK_FLAGS:
            return "git add {} stages the whole tree (private dirs ride along)".format(tok)
        if tok.startswith("-"):
            # other flags (e.g. -n dry-run, -v) are harmless on their own.
            continue
        stripped = tok.strip("'\"")
        if stripped in _BULK_PATHSPECS:
            return "git add '{}' stages everything (private dirs ride along)".format(stripped)
        path_seen = True
        if is_private(stripped):
            return "git add '{}' would stage a private / generated artifact".format(stripped)

    # bare ``git add`` with no pathspec is interactive/no-op; leave it alone.
    if not path_seen:
        return None
    return None


def main():
    event = read_event()
    tool_input = (event or {}).get("tool_input") or {}
    cmd = tool_input.get("command")
    if not cmd or not cmd.strip():
        return allow()

    for seg in _segments(cmd):
        name, args = _tokens_after_name(seg)
        if name != "git":
            continue
        reason = _check_add(args)
        if reason:
            return deny(
                "Blocked: refusing to stage private/generated files ({}). Stage "
                "explicit project paths instead of bulk-adding, and never commit "
                "reports/, .cache/, logs/, outputs/, or .env*.".format(reason)
            )

    return allow()


if __name__ == "__main__":
    main()
