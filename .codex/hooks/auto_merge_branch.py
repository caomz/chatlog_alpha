#!/usr/bin/env python3
"""Deprecated compatibility shim for stale Codex Stop hook caches.

The active hook configuration no longer calls this file. Some already-running
Codex Desktop sessions may keep the previous Stop hook command in memory until
the app reloads its hook config. Keep this script as a harmless no-op so those
stale calls do not fail.
"""

from __future__ import annotations

import sys


def main() -> int:
    print(
        "[chatlog-auto-merge] deprecated no-op: auto merge hook was removed; "
        "reload Codex to drop stale cached hook commands.",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
