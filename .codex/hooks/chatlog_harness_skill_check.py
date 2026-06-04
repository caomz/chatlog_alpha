#!/usr/bin/env python3
"""Project-scoped Codex hook for chatlog_alpha harness skill checks."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any

PROJECT_ROOT = Path("/Volumes/WorkSSD/Dev/chatlog_alpha").resolve()
CHECK_CMDS = [
    {
        "label": "repo-local harness skill",
        "command": ["node", "skills/chatlog-http-cli/scripts/check-harness-skill.mjs"],
        "failure": "fix skills/chatlog-http-cli before declaring completion.",
    },
    {
        "label": "root harness bridge",
        "command": ["node", "scripts/check-root-harness.mjs"],
        "failure": "fix root harness artifacts before declaring completion.",
    },
]


def _load_payload() -> dict[str, Any]:
    raw = sys.stdin.read()
    if not raw.strip():
        return {}
    try:
        payload = json.loads(raw)
        return payload if isinstance(payload, dict) else {}
    except json.JSONDecodeError:
        return {}


def _candidate_paths(payload: dict[str, Any]) -> list[Path]:
    values: list[str] = []
    for key in ("cwd", "workdir", "workspace", "currentWorkingDirectory"):
        value = payload.get(key)
        if isinstance(value, str):
            values.append(value)

    env_keys = ("CODEX_CWD", "CODEX_WORKSPACE", "PWD")
    values.extend(value for key in env_keys if (value := os.environ.get(key)))
    values.append(os.getcwd())

    paths: list[Path] = []
    for value in values:
        try:
            paths.append(Path(value).expanduser().resolve())
        except OSError:
            continue
    return paths


def _is_inside_project(path: Path) -> bool:
    return path == PROJECT_ROOT or PROJECT_ROOT in path.parents


def _run_check(label: str, command: list[str], failure: str) -> int:
    print(f"[chatlog harness] running {label} check...")
    try:
        result = subprocess.run(
            command,
            cwd=PROJECT_ROOT,
            text=True,
            capture_output=True,
            timeout=30,
            check=False,
        )
    except FileNotFoundError as exc:
        print(f"[chatlog harness] failed to start {label} check: {exc}", file=sys.stderr)
        return 1
    except subprocess.TimeoutExpired:
        print(f"[chatlog harness] {label} check timed out after 30s", file=sys.stderr)
        return 1

    if result.stdout:
        print(result.stdout.rstrip())
    if result.stderr:
        print(result.stderr.rstrip(), file=sys.stderr)

    if result.returncode != 0:
        print(
            f"[chatlog harness] FAILED: {failure}",
            file=sys.stderr,
        )
        return result.returncode

    print(f"[chatlog harness] OK: {label} check passed.")
    return 0


def main() -> int:
    payload = _load_payload()
    if not any(_is_inside_project(path) for path in _candidate_paths(payload)):
        return 0

    for check in CHECK_CMDS:
        code = _run_check(check["label"], check["command"], check["failure"])
        if code != 0:
            return code

    print("[chatlog harness] OK: all harness checks passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
