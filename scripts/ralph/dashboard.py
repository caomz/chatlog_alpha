#!/usr/bin/env python3
"""
Ralph Dashboard - 实时监控面板
启动一个本地 HTTP 服务，服务 dashboard.html 并提供 /api/state 接口。
"""

import argparse
import json
import threading
import webbrowser
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

SCRIPT_DIR = Path(__file__).parent.resolve()
PRD_FILE = SCRIPT_DIR / "prd.json"
PROGRESS_FILE = SCRIPT_DIR / "progress.txt"
HTML_FILE = SCRIPT_DIR / "dashboard.html"
PIXEL_HTML_FILE = SCRIPT_DIR / "dashboard-p.html"

_state: dict = {
    "iteration": 0,
    "max_iterations": 50,
    "phase": "idle",       # idle | developing | validating | done | error
    "current_story": None,
    "started_at": None,
}
_state_lock = threading.Lock()


def set_state(
    iteration: int | None = None,
    phase: str | None = None,
    current_story: str | None = None,
) -> None:
    with _state_lock:
        if iteration is not None:
            _state["iteration"] = iteration
        if phase is not None:
            _state["phase"] = phase
        if current_story is not None:
            _state["current_story"] = current_story


def _build_api_response() -> dict:
    with _state_lock:
        s = dict(_state)

    elapsed = int(time.time() - s["started_at"]) if s["started_at"] else 0

    project = ""
    branch_name = ""
    stories = []
    try:
        prd = json.loads(PRD_FILE.read_text(encoding="utf-8"))
        project = prd.get("project", "")
        branch_name = prd.get("branchName", "")
        stories = prd.get("userStories", [])
    except Exception:
        pass

    logs = ""
    try:
        if PROGRESS_FILE.exists():
            logs = PROGRESS_FILE.read_text(encoding="utf-8")
    except Exception:
        pass

    return {
        "runtime": {
            "iteration": s["iteration"],
            "max_iterations": s["max_iterations"],
            "phase": s["phase"],
            "current_story": s["current_story"],
            "elapsed": elapsed,
        },
        "project": project,
        "branchName": branch_name,
        "stories": stories,
        "logs": logs,
    }


class _Handler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        path = self.path.split("?")[0]

        if path == "/api/state":
            body = json.dumps(_build_api_response(), ensure_ascii=False).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Access-Control-Allow-Origin", "*")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        elif path in ("/", "/index.html"):
            try:
                html = HTML_FILE.read_bytes()
                self.send_response(200)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.send_header("Content-Length", str(len(html)))
                self.end_headers()
                self.wfile.write(html)
            except Exception as e:
                msg = str(e).encode()
                self.send_response(500)
                self.send_header("Content-Length", str(len(msg)))
                self.end_headers()
                self.wfile.write(msg)

        elif path in ("/p", "/p.html"):
            try:
                html = PIXEL_HTML_FILE.read_bytes()
                self.send_response(200)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.send_header("Content-Length", str(len(html)))
                self.end_headers()
                self.wfile.write(html)
            except Exception as e:
                msg = str(e).encode()
                self.send_response(500)
                self.send_header("Content-Length", str(len(msg)))
                self.end_headers()
                self.wfile.write(msg)

        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, format: str, *args) -> None:  # suppress access logs
        pass


def start(
    port: int = 7331,
    max_iterations: int = 50,
    open_browser: bool = True,
    block: bool = False,
) -> HTTPServer:
    with _state_lock:
        _state["started_at"] = time.time()
        _state["max_iterations"] = max_iterations

    server = HTTPServer(("127.0.0.1", port), _Handler)
    url = f"http://localhost:{port}"

    if block:
        if open_browser:
            threading.Timer(0.8, lambda: webbrowser.open(url)).start()
        print(f"🖥️  Dashboard: {url} (standalone)")
        print("Press Ctrl+C to stop.")
        return server

    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()

    print(f"🖥️  Dashboard: {url}")

    if open_browser:
        threading.Timer(0.8, lambda: webbrowser.open(url)).start()

    # Keep server object for caller-managed shutdown.
    return server


def main() -> None:
    parser = argparse.ArgumentParser(description="Run Ralph dashboard for progress visualization")
    parser.add_argument("--port", type=int, default=7331, help="Dashboard service port")
    parser.add_argument(
        "--max-iterations",
        type=int,
        default=50,
        help="Max iterations to expose in /api/state",
    )
    parser.add_argument(
        "--no-browser",
        action="store_true",
        help="Do not open browser automatically when running standalone",
    )

    args = parser.parse_args()
    server = start(
        port=args.port,
        max_iterations=args.max_iterations,
        open_browser=not args.no_browser,
        block=True,
    )
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        print("🛑 Dashboard stopped")
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    main()
