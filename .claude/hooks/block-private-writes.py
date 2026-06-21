#!/usr/bin/env python3
"""block-private-writes: PreToolUse guard for Write|Edit|MultiEdit.

Reads the tool_input.file_path from stdin and blocks writes that target
private / generated artifact paths (reports/, .cache/, logs/, outputs/,
.env, etc.). Two narrow allowlists keep the guard self-bootstrapping:

- .claude/hooks/ — so we can edit this very hook without deadlocking
  on hot reload.
- .claude/settings.json — so the wiring file can be (re)generated.

Everything else is decided by _common.is_private / _common.rel_to_project.
"""

import os
import sys

# Make sibling _common importable regardless of how the harness invokes us.
_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

from _common import allow, deny, is_private, read_event, rel_to_project  # noqa: E402

# Narrow allowlist: hook code and wiring config can be (re)written by the
# agent. rel_to_project() strips CLAUDE_PROJECT_DIR + leading './' + leading
# '/', so the candidates below appear as '.claude/hooks/...' or
# '.claude/settings.json' from the test fixtures in prd.json US-002.
def _is_self_or_wiring(rel):
    """Return True when ``rel`` is this hook file or settings.json."""
    if not rel:
        return False
    if rel.startswith(".claude/hooks/") or rel.startswith(".claude\\hooks\\"):
        return True
    if rel == ".claude/settings.json" or rel == ".claude\\settings.json":
        return True
    return False


def main():
    event = read_event()
    tool_input = (event or {}).get("tool_input") or {}
    file_path = tool_input.get("file_path")
    if not file_path:
        # Missing file_path is harmless — let the tool itself complain.
        return allow()

    rel = rel_to_project(file_path)
    if _is_self_or_wiring(rel):
        return allow()

    if is_private(rel):
        return deny(
            "Blocked: refusing to write private path '{}' "
            "(reports/ / .cache/ / logs/ / outputs/ / .env* / wcdb_cache/ "
            "are generated artifacts — do not overwrite).".format(rel)
        )

    return allow()


if __name__ == "__main__":
    main()