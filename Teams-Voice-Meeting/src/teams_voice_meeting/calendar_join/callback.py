"""Receiver for Graph call notifications (the public endpoint of calendar mode).

Informational only. Notifications are not authenticated in this MVP, so nothing here changes what the
scheduler does; it only logs and records the last state of each call.
"""

from __future__ import annotations

import json
import logging
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

log = logging.getLogger(__name__)
MAX_BODY = 1_000_000


class CallRegistry:
    def __init__(self) -> None:
        self.states: dict[str, str] = {}

    def update(self, payload: dict) -> None:
        for n in payload.get("value", []):
            call_id = (n.get("resourceUrl") or "").rstrip("/").rsplit("/", 1)[-1]
            state = (n.get("resourceData") or {}).get("state") or n.get("changeType")
            if call_id and state:
                self.states[call_id] = state
                log.info("call %s: %s", call_id, state)


def make_server(host: str, port: int, registry: CallRegistry) -> ThreadingHTTPServer:
    class Handler(BaseHTTPRequestHandler):
        def _reply(self, code: int, body: bytes = b"{}") -> None:
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            self._reply(200 if self.path == "/healthz" else 404)

        def do_POST(self):
            length = int(self.headers.get("Content-Length") or 0)
            if length > MAX_BODY:
                return self._reply(413)
            try:
                registry.update(json.loads(self.rfile.read(length) or b"{}"))
            except ValueError:
                return self._reply(400)
            self._reply(202)

        def log_message(self, *args):  # keep request lines out of stderr
            pass

    return ThreadingHTTPServer((host, port), Handler)


def serve_in_background(listen: str, registry: CallRegistry) -> ThreadingHTTPServer:
    host, _, port = listen.rpartition(":")
    server = make_server(host or "0.0.0.0", int(port), registry)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server
