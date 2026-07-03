"""llama.cpp server への httpx クライアント — TTFT/総時間計測と CoT 分離を担う."""

import json
import time
from types import TracebackType
from typing import Any, Self

import httpx
from pydantic import BaseModel

from moka_eval.config import Sampling

THINK_CLOSE = "</think>"
THINK_OPEN = "<think>"


class LlamaClientError(RuntimeError):
    """llama.cpp server との対話失敗."""


def split_think(text: str) -> tuple[str | None, str]:
    """CoT と最終回答を分離する(bp-python §10)。除去前テキストで測定しないこと.

    - 最後の ``</think>`` で分割(思考が複数回出るモデル対策)
    - 閉じタグなしで開きタグあり(生成打ち切り)→ 全文を CoT、回答は空
    - 開きタグなしで閉じタグあり(サーバがタグを吸収)→ 閉じタグ前を CoT
    - タグ皆無 → CoT なし
    """
    if THINK_CLOSE in text:
        cot_part, _, answer = text.rpartition(THINK_CLOSE)
        cot = cot_part.replace(THINK_OPEN, "").strip()
        return cot, answer.strip()
    if THINK_OPEN in text:
        return text.replace(THINK_OPEN, "").strip(), ""
    return None, text.strip()


class ChatResult(BaseModel):
    content: str
    cot: str | None
    answer: str
    ttft_ms: float
    total_ms: float
    prompt_tokens: int
    predicted_tokens: int
    finish_reason: str
    timings: dict[str, float] | None


class LlamaClient:
    """使い回す httpx.Client のラッパ(bp-python §8)."""

    def __init__(self, base_url: str, *, timeout_s: float = 900.0) -> None:
        self.base_url = base_url.rstrip("/")
        self._http = httpx.Client(
            base_url=self.base_url,
            timeout=httpx.Timeout(timeout_s, connect=10.0),
        )

    def __enter__(self) -> Self:
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        self.close()

    def close(self) -> None:
        self._http.close()

    def health(self) -> bool:
        try:
            return self._http.get("/health").status_code == 200
        except httpx.HTTPError:
            return False

    def tokenize_count(self, text: str) -> int:
        resp = self._http.post("/tokenize", json={"content": text})
        resp.raise_for_status()
        tokens: list[int] = resp.json()["tokens"]
        return len(tokens)

    def embed(self, texts: list[str]) -> list[list[float]]:
        resp = self._http.post("/v1/embeddings", json={"input": texts, "model": "eval"})
        resp.raise_for_status()
        data = sorted(resp.json()["data"], key=lambda d: d["index"])
        return [d["embedding"] for d in data]

    def completion_native(self, prompt: str, *, n_predict: int, seed: int = 42) -> dict[str, Any]:
        """native /completion — timings が確実に返る(bench 用)."""
        resp = self._http.post(
            "/completion",
            json={
                "prompt": prompt,
                "n_predict": n_predict,
                "temperature": 0.0,
                "seed": seed,
                "cache_prompt": False,
            },
        )
        resp.raise_for_status()
        result: dict[str, Any] = resp.json()
        return result

    def chat(
        self,
        prompt: str,
        *,
        sampling: Sampling,
        seed: int = 42,
        max_tokens: int | None = None,
        json_schema: dict[str, Any] | None = None,
        system: str | None = None,
        chat_template_kwargs: dict[str, bool | str] | None = None,
    ) -> ChatResult:
        """ストリーミングで TTFT / 総時間を計測しつつ 1 回生成する."""
        messages: list[dict[str, str]] = []
        if system is not None:
            messages.append({"role": "system", "content": system})
        messages.append({"role": "user", "content": prompt})

        body: dict[str, Any] = {
            "messages": messages,
            "stream": True,
            "stream_options": {"include_usage": True},
            "seed": seed,
            **sampling.as_request_params(),
        }
        if max_tokens is not None:
            body["max_tokens"] = max_tokens
        if chat_template_kwargs is not None:
            body["chat_template_kwargs"] = chat_template_kwargs
        if json_schema is not None:
            body["response_format"] = {
                "type": "json_schema",
                "json_schema": {"name": "result", "schema": json_schema, "strict": True},
            }

        content_parts: list[str] = []
        reasoning_parts: list[str] = []
        finish_reason = "unknown"
        prompt_tokens = 0
        predicted_tokens = 0
        timings: dict[str, float] | None = None
        ttft_ms: float | None = None

        started = time.monotonic()
        with self._http.stream("POST", "/v1/chat/completions", json=body) as resp:
            if resp.status_code != 200:
                detail = resp.read().decode(errors="replace")[:500]
                msg = f"chat failed: HTTP {resp.status_code}: {detail}"
                raise LlamaClientError(msg)
            for line in resp.iter_lines():
                if not line.startswith("data: "):
                    continue
                payload = line[len("data: ") :]
                if payload == "[DONE]":
                    break
                chunk: dict[str, Any] = json.loads(payload)
                if isinstance(chunk.get("timings"), dict):
                    timings = {
                        k: float(v)
                        for k, v in chunk["timings"].items()
                        if isinstance(v, (int, float))
                    }
                usage = chunk.get("usage")
                if isinstance(usage, dict):
                    prompt_tokens = int(usage.get("prompt_tokens", 0))
                    predicted_tokens = int(usage.get("completion_tokens", 0))
                for choice in chunk.get("choices", []):
                    if choice.get("finish_reason"):
                        finish_reason = str(choice["finish_reason"])
                    delta = choice.get("delta", {})
                    piece = delta.get("content")
                    thought = delta.get("reasoning_content")
                    if piece:
                        content_parts.append(piece)
                    if thought:
                        reasoning_parts.append(thought)
                    if (piece or thought) and ttft_ms is None:
                        ttft_ms = (time.monotonic() - started) * 1000.0
        total_ms = (time.monotonic() - started) * 1000.0

        content = "".join(content_parts)
        if reasoning_parts:
            cot: str | None = "".join(reasoning_parts).strip()
            answer = content.strip()
        else:
            cot, answer = split_think(content)
        return ChatResult(
            content=content,
            cot=cot,
            answer=answer,
            ttft_ms=ttft_ms if ttft_ms is not None else total_ms,
            total_ms=total_ms,
            prompt_tokens=prompt_tokens,
            predicted_tokens=predicted_tokens,
            finish_reason=finish_reason,
            timings=timings,
        )
