#!/usr/bin/env python3
"""E2E 用の OpenAI 互換チャット補完モック(stdlib のみ、pip install 不要)。

core/internal/llm.Client(summarize/tags/埋め込み共通の HTTP 機構)が話す最小限だけを実装する:
POST {LLM_BASE_URL}/chat/completions(非ストリーム・SSE ストリームの両方)、
POST {LLM_BASE_URL}/embeddings(OpenAI 互換、1024次元 = db/schema.sql の vector(1024))、
GET /health。
response_format(json_schema、タグ抽出が使う)を検知したら tags 用の決定的な JSON を返す —
それ以外(要約)は自由形式の固定文言を返す。
埋め込みは入力文字列だけから決定的に作る(文字 3-gram の feature hashing)ので、同じ入力は
常に同じベクトルになり、字面が重なる入力どうしは cosine が近くなる — 本物の意味埋め込みの
代替ではないが、enrich.Scheduler の埋め込み濃縮とハイブリッド検索のベクトル側の配線
(moka-core → /embeddings → article_embeddings → pgvector 近傍)を決定的に検証できる。
本物の llm(llama.cpp server, compose.yaml)と同じ 8081/health 契約に合わせてある。
GitHub-hosted runner には GPU が無く本物の llm(iGPU Vulkan passthrough)を起動できないため、
CI の E2E ではこのモックを compose.e2e.yaml 経由で LLM_BASE_URL に差し込む。
"""

import hashlib
import json
import math
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(os.environ.get("PORT", "8081"))
MODEL = os.environ.get("MOCK_LLM_MODEL", "mock-model")
CONTENT = os.environ.get("MOCK_LLM_CONTENT", "これはE2E用のモックLLM応答です。")
TAGS = os.environ.get("MOCK_LLM_TAGS", "E2Eタグ1,E2Eタグ2")
EMBED_DIM = 1024  # db/schema.sql: article_embeddings.embedding vector(1024)


def embed_vector(text):
    """入力文字列から決定的な単位ベクトルを作る(文字 3-gram の feature hashing)。

    各 3-gram を SHA-256 で 1024 次元のバケツ + 符号に写して足し込み、L2 正規化する。
    乱数・時刻・プロセス状態に依存しないので、同じ入力は常に同じベクトル(E2E の安定性)。
    """
    vec = [0.0] * EMBED_DIM
    padded = f"  {text}  "
    for i in range(len(padded) - 2):
        digest = hashlib.sha256(padded[i : i + 3].encode("utf-8")).digest()
        idx = int.from_bytes(digest[:4], "big") % EMBED_DIM
        sign = 1.0 if digest[4] % 2 == 0 else -1.0
        vec[idx] += sign
    norm = math.sqrt(sum(v * v for v in vec))
    if norm == 0.0:
        vec[0] = 1.0
        norm = 1.0
    return [v / norm for v in vec]


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
        if not (self.path.endswith("/chat/completions") or self.path.endswith("/embeddings")):
            self._json(404, {"error": "not found"})
            return

        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length else b"{}"
        try:
            req = json.loads(raw or b"{}")
        except json.JSONDecodeError:
            req = {}

        if self.path.endswith("/embeddings"):
            self._embeddings_response(req)
        elif req.get("response_format", {}).get("type") == "json_schema":
            self._full_response(self._tags_content())
        elif req.get("stream"):
            self._stream_response()
        else:
            self._full_response(CONTENT)

    def _embeddings_response(self, req):
        # OpenAI 互換: input は文字列 or 文字列配列(moka-core は文字列 1 本で送る)
        raw_input = req.get("input", "")
        inputs = raw_input if isinstance(raw_input, list) else [raw_input]
        data = [
            {"object": "embedding", "index": i, "embedding": embed_vector(str(text))}
            for i, text in enumerate(inputs)
        ]
        self._json(
            200,
            {
                "object": "list",
                "model": req.get("model") or MODEL,
                "data": data,
                "usage": {"prompt_tokens": 0, "total_tokens": 0},
            },
        )

    def _tags_content(self):
        names = [t.strip() for t in TAGS.split(",") if t.strip()]
        return json.dumps({"tags": names}, ensure_ascii=False)

    def _full_response(self, content):
        body = {
            "model": MODEL,
            "choices": [{"message": {"role": "assistant", "content": content}}],
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
