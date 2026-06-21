#!/usr/bin/env python3
"""guard-quota-commands: PreToolUse guard for Bash.

Reads tool_input.command from stdin and blocks commands that quietly burn model
quota or expose prompts to a provider. AGENTS.md states these must only run when
the task and the user explicitly require them, so the default is to deny:

- ``chatlog report daily --vision`` / ``--summary`` — trigger vision / summary
  model calls (also via ``go run . report daily ...``).
- ``chatlog semantic test`` — issues a live model probe.
- any command hitting ``/api/v1/semantic/test`` (e.g. a curl) — same live probe.

Safe variants always pass: ``--help`` (no model call), plain ``chatlog report
daily`` (no vision/summary), and read-only commands like ``chatlog http list``.
"""

import os
import re
import sys

# Make sibling _common importable regardless of how the harness invokes us.
_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

from _common import allow, deny, read_event  # noqa: E402

# The semantic test HTTP endpoint always consumes quota; block it outright.
_QUOTA_ENDPOINT = "/api/v1/semantic/test"


def _segments(cmd):
    """Split a command line into pipeline / sequence segments."""
    return [s for s in re.split(r"[|;&\n()]+", cmd) if s.strip()]


def _check_segment(seg):
    """Return a deny reason for a quota-sensitive segment, else None."""
    # The semantic test endpoint is quota-sensitive regardless of any --help.
    if _QUOTA_ENDPOINT in seg:
        return "hits {} (issues a live semantic model probe)".format(_QUOTA_ENDPOINT)

    tokens = seg.split()
    # --help / -h never calls a model; always safe to inspect command usage.
    if "--help" in tokens or "-h" in tokens:
        return None

    # ``report daily --vision`` / ``--summary`` (chatlog or go run . report ...).
    if "report" in tokens and "daily" in tokens:
        if "--vision" in tokens:
            return "report daily --vision triggers vision model calls (consumes quota)"
        if "--summary" in tokens:
            return "report daily --summary triggers summary model calls (consumes quota)"

    # ``chatlog semantic test`` issues a live model probe.
    if "semantic" in tokens and "test" in tokens:
        return "semantic test issues a live model probe (consumes quota)"

    return None


def main():
    event = read_event()
    tool_input = (event or {}).get("tool_input") or {}
    cmd = tool_input.get("command")
    if not cmd or not cmd.strip():
        return allow()

    for seg in _segments(cmd):
        reason = _check_segment(seg)
        if reason:
            return deny(
                "Blocked: refusing quota/privacy-sensitive command ({}). Run it "
                "only when the task and the user explicitly require model/vision "
                "calls; use --help or the non-summary path otherwise.".format(reason)
            )

    return allow()


if __name__ == "__main__":
    main()
