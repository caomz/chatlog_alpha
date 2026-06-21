#!/usr/bin/env python3
"""block-private-reads: PreToolUse guard for Bash.

Reads the tool_input.command from stdin and blocks shell commands that would
print the *contents* of a private / generated artifact (reports/, .cache/,
logs/, outputs/, .env*, wcdb_cache/) to the transcript.

Two classes of commands are considered:

- Content-reading commands (cat / head / tail / jq / sqlite3): blocked only
  when a private path token is present in the command.
- grep family (grep / egrep / fgrep): blocked unconditionally — grep prints
  matched lines, and a non-private-looking path may still resolve to private
  data, so we stay conservative ("grep 一律拦").

Metadata-only commands (ls / wc / stat / file / du …) are NOT read commands,
so they are allowed even when they target a private directory.
"""

import os
import re
import sys

# Make sibling _common importable regardless of how the harness invokes us.
_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

from _common import allow, deny, is_private, read_event  # noqa: E402

# Commands that dump file contents; blocked when paired with a private path.
READ_CMDS = {"cat", "head", "tail", "jq", "sqlite3"}
# grep family is blocked unconditionally — see module docstring.
GREP_CMDS = {"grep", "egrep", "fgrep"}


def _command_names(cmd):
    """Extract executable names from each pipeline / sequence segment.

    Splits on shell separators (| ; & newline parens), skips leading
    VAR=value assignments, and strips any directory prefix so that
    '/usr/bin/cat' is recognised as 'cat'.
    """
    names = []
    for seg in re.split(r"[|;&\n()]+", cmd):
        tokens = seg.strip().split()
        i = 0
        # skip leading environment assignments like FOO=bar cat ...
        while i < len(tokens) and "=" in tokens[i] and not tokens[i].startswith("-"):
            i += 1
        if i < len(tokens):
            names.append(tokens[i].rsplit("/", 1)[-1])
    return names


def _path_tokens(cmd):
    """Return command tokens that could be path arguments (skip flags)."""
    toks = []
    for raw in re.split(r"\s+", cmd):
        t = raw.strip().strip("'\"")
        if not t or t.startswith("-"):
            continue
        toks.append(t)
    return toks


def main():
    event = read_event()
    tool_input = (event or {}).get("tool_input") or {}
    cmd = tool_input.get("command")
    if not cmd or not cmd.strip():
        return allow()

    names = set(_command_names(cmd))

    # grep family: conservatively always block — it prints matched file content.
    if names & GREP_CMDS:
        return deny(
            "Blocked: refusing to run grep — it prints matched file content "
            "which may include private chat-derived data. Verify by path / "
            "count / metadata instead (ls, wc, stat)."
        )

    # Other content-reading commands: block only when a private path is present.
    if names & READ_CMDS:
        for tok in _path_tokens(cmd):
            if is_private(tok):
                return deny(
                    "Blocked: refusing to print private path '{}' "
                    "(reports/ / .cache/ / logs/ / outputs/ / .env* / "
                    "wcdb_cache/ are private — verify by metadata, not "
                    "content).".format(tok)
                )

    return allow()


if __name__ == "__main__":
    main()
