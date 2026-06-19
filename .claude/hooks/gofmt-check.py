#!/usr/bin/env python3
"""gofmt-check: PostToolUse reminder for Write|Edit|MultiEdit on .go files.

After the agent writes a .go file we run ``gofmt -l <file>``. gofmt -l prints
the path of any file whose formatting differs from gofmt's output, so a
non-empty result means the file is not formatted. We then exit 2 with a
reminder on stderr (PostToolUse exit 2 surfaces stderr back to the model) so
the agent knows to run ``gofmt -w`` — this is a reminder, not a hard block:
the write has already happened.

Safety:
- Only .go files trigger gofmt; anything else exits 0.
- If gofmt is not on PATH (FileNotFoundError) we exit 0 — a missing toolchain
  must never turn into noise on every edit.
"""

import os
import subprocess
import sys

# Make sibling _common importable regardless of how the harness invokes us.
_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

from _common import allow, read_event  # noqa: E402


def main():
    event = read_event()
    tool_input = (event or {}).get("tool_input") or {}
    file_path = tool_input.get("file_path")
    if not file_path or not str(file_path).endswith(".go"):
        # Not a Go file — nothing to check.
        return allow()

    try:
        proc = subprocess.run(
            ["gofmt", "-l", str(file_path)],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            timeout=10,
        )
    except FileNotFoundError:
        # gofmt not installed — do not crash, do not nag.
        return allow()
    except Exception:
        # Any other failure (timeout, etc.) is non-fatal: stay silent.
        return allow()

    out = (proc.stdout or b"").decode("utf-8", "replace").strip()
    if out:
        # gofmt -l listed the file → it is not formatted.
        sys.stderr.write(
            "Reminder: '{}' is not gofmt-formatted. "
            "Run 'gofmt -w {}' before finishing.\n".format(file_path, file_path)
        )
        sys.exit(2)

    return allow()


if __name__ == "__main__":
    main()
