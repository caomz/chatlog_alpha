#!/usr/bin/env python3
"""Shared helpers for chatlog_alpha Claude Code guard hooks.

These hooks enforce the hard rules that AGENTS.md / CLAUDE.md only state in
prose (private read/write, batch delete, destructive git, committing private
reports, quota-sensitive commands, gofmt, DoD state updates).

Design principles:
- Hooks must NEVER crash a tool call on malformed input: when stdin is empty or
  not valid JSON we exit 0 (allow), so a parsing bug can never block the agent.
- Path checks normalise Windows backslashes so the same rules apply on macOS and
  Windows WeChat data layouts.
"""

import json
import os
import sys


def _norm(path):
    """Normalise a path string: convert Windows backslashes to '/'.

    兼容 Windows 反斜杠路径: 先把双反斜杠再把单反斜杠都换成 '/'.
    """
    if path is None:
        return ""
    # path.replace(chr(92)+chr(92), '/') handles JSON-escaped '\\'; the second
    # replace catches a lone backslash after JSON has already unescaped it.
    return path.replace(chr(92) + chr(92), "/").replace(chr(92), "/")


def read_event():
    """Read and parse the hook event JSON from stdin.

    On empty stdin or invalid JSON, exit 0 (allow) — a hook must never block a
    tool call just because the harness fed it something unexpected.
    """
    try:
        data = sys.stdin.read()
    except Exception:
        sys.exit(0)
    if not data or not data.strip():
        sys.exit(0)
    try:
        return json.loads(data)
    except Exception:
        sys.exit(0)


def deny(reason):
    """Block the tool call: write the reason to stderr and exit 2.

    Exit code 2 is the Claude Code convention for "block this PreToolUse call
    and surface stderr back to the model".
    """
    sys.stderr.write(str(reason) + "\n")
    sys.exit(2)


def allow():
    """Allow the tool call: exit 0 with no output."""
    sys.exit(0)


def rel_to_project(path):
    """Return ``path`` relative to the project root.

    Strips the ``$CLAUDE_PROJECT_DIR`` (or cwd) prefix and any leading ``./``.
    Always normalises Windows backslashes first.
    """
    p = _norm(path)
    root = _norm(os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd())
    if root and p.startswith(root):
        p = p[len(root):]
    p = p.lstrip("/")
    if p.startswith("./"):
        p = p[2:]
    return p


def is_private(rel_path):
    """Return True if ``rel_path`` points at a private / generated artifact.

    Private = the directories and files AGENTS.md forbids committing or printing:
    reports/, reports.backup-*, .cache/, logs/, outputs/, and any .env* except
    .env.example. Decrypted WeChat caches under wcdb_cache/ are also private.
    """
    p = _norm(rel_path)
    if root := os.environ.get("CLAUDE_PROJECT_DIR"):
        root = _norm(root)
        if root and p.startswith(root):
            p = p[len(root):]
    p = p.lstrip("/")
    if p.startswith("./"):
        p = p[2:]
    if not p:
        return False

    base = p.rsplit("/", 1)[-1]

    # .env family: .env and .env.<anything> are private, except .env.example.
    if base == ".env":
        return True
    if base.startswith(".env.") and base != ".env.example":
        return True

    # Private directory prefixes.
    private_prefixes = (
        "reports/",
        ".cache/",
        "logs/",
        "outputs/",
        "wcdb_cache/",
    )
    if p.startswith(private_prefixes):
        return True

    # reports.backup-<timestamp>/... snapshots.
    if p.startswith("reports.backup"):
        return True

    return False
