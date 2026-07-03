#!/usr/bin/env python3
"""E2E 用の OpenAI 互換チャット補完モック(stdlib のみ、pip install 不要)。

core/internal/summarize/client.go(HTTPCompleter)が話す最小限だけを実装する:
POST {LLM_BASE_URL}/chat/completions(非ストリーム・SSE ストリームの両方)と GET /health。
本物の llm(llama.cpp server, compose.yaml)と同じ 8081/health 契約に合わせてある。
GitHub-hosted runner には GPU が無く本物の llm(iGPU Vulkan passthrough)を起動できないため、
CI の E2E ではこのモックを compose.e2e.yaml 経由で LLM_BASE_URL に差し込む。
"""

import json
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(os.environ.get("PORT", "8081"))
MODEL = os.environ.get("MOCK_LLM_MODEL", "mock-model")
CONTENT = os.environ.get("MOCK_LLM_CONTENT", "これはE2E用のモックLLM応答です。")


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        sys.stderr.write("mock-llm: " + (fmt % args) + "\n")

    def do_GET(self):
        if self.path == "/health":
            self._json(200, {"status": "ok"})
            return
        self._json(404, {"error": "not found"})

    def do_POST(self):
        if not self.path.endswith("/chat/completions"):
            self._json(404, {"error": "not found"})
            return

        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length else b"{}"
        try:
            req = json.loads(raw or b"{}")
        except json.JSONDecodeError:
            req = {}

        if req.get("stream"):
            self._stream_response()
        else:
            self._full_response()

    def _full_response(self):
        body = {
            "model": MODEL,
            "choices": [{"message": {"role": "assistant", "content": CONTENT}}],
        }
        self._json(200, body)

    def _stream_response(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "close")
        self.end_headers()
        self.close_connection = True

        words = CONTENT.split(" ") or [CONTENT]
        chunk_size = max(1, len(words) // 4)
        for i in range(0, len(words), chunk_size):
            piece = " ".join(words[i : i + chunk_size])
            if i + chunk_size < len(words):
                piece += " "
            self._write_sse({"model": MODEL, "choices": [{"delta": {"content": piece}}]})
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()

    def _write_sse(self, obj):
        line = f"data: {json.dumps(obj, ensure_ascii=False)}\n\n"
        self.wfile.write(line.encode("utf-8"))
        self.wfile.flush()

    def _json(self, status, obj):
        body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    server = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    sys.stderr.write(f"mock-llm listening on :{PORT}\n")
    server.serve_forever()


if __name__ == "__main__":
    main()
