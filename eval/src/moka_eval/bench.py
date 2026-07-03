"""速度実測 — pp/tg 分離(tenets §9 形式)、-np 並列劣化率、VRAMスナップショット."""

import asyncio
import time
from datetime import UTC, datetime
from typing import Any

import httpx

from moka_eval.client import LlamaClient
from moka_eval.config import LLAMACPP_BUILD, ModelSpec
from moka_eval.gpu import gib, mem_snapshot
from moka_eval.records import BenchRecord

_BENCH_SENTENCE = (
    "モカはエージェント型RSSリーダーであり、記事の取得と保存を核パスとして、"
    "要約・タグ抽出・翻訳などの濃縮処理をローカルLLMで行う設計になっている。"
)


def bench_prompt(target_tokens: int = 512) -> str:
    """~512トークン相当の固定日本語プロンプト(約60字×n文で概算)."""
    reps = max(1, target_tokens // 40)
    body = _BENCH_SENTENCE * reps
    return f"次の文章を読んで要点を述べよ。\n{body}"


class BenchError(RuntimeError):
    """ベンチ実行の失敗."""


def _record_from_timings(
    spec: ModelSpec, timings: dict[str, Any], *, np: int, rep: int
) -> BenchRecord:
    snap = mem_snapshot()
    return BenchRecord(
        model_key=spec.key,
        model_hf=spec.hf,
        llamacpp_build=LLAMACPP_BUILD,
        np=np,
        rep=rep,
        pp_n=int(timings.get("prompt_n", 0)),
        pp_ms=float(timings.get("prompt_ms", 0.0)),
        pp_tps=round(float(timings.get("prompt_per_second", 0.0)), 1),
        tg_n=int(timings.get("predicted_n", 0)),
        tg_ms=float(timings.get("predicted_ms", 0.0)),
        tg_tps=round(float(timings.get("predicted_per_second", 0.0)), 1),
        vram_used_gib=gib(snap["vram_used"]) if "vram_used" in snap else None,
        gtt_used_gib=gib(snap["gtt_used"]) if "gtt_used" in snap else None,
        created_at=datetime.now(UTC).isoformat(timespec="seconds"),
    )


def run_bench(
    client: LlamaClient, spec: ModelSpec, *, reps: int = 3, n_predict: int = 128
) -> list[BenchRecord]:
    """warmup 1回を捨てて reps 回、native /completion の timings を採る."""
    prompt = bench_prompt()
    client.completion_native(prompt, n_predict=16)  # warmup(捨てる)
    records: list[BenchRecord] = []
    for rep in range(reps):
        result = client.completion_native(prompt, n_predict=n_predict)
        timings = result.get("timings")
        if not isinstance(timings, dict):
            msg = "no timings in /completion response"
            raise BenchError(msg)
        records.append(_record_from_timings(spec, timings, np=1, rep=rep))
    return records


async def _one_stream(http: httpx.AsyncClient, prompt: str, n_predict: int) -> dict[str, Any]:
    resp = await http.post(
        "/completion",
        json={
            "prompt": prompt,
            "n_predict": n_predict,
            "temperature": 0.0,
            "seed": 42,
            "cache_prompt": False,
        },
    )
    resp.raise_for_status()
    result: dict[str, Any] = resp.json()
    return result


def run_np_bench(
    base_url: str, spec: ModelSpec, *, np: int = 3, n_predict: int = 128
) -> dict[str, Any]:
    """-np で起動済みのサーバに np 本の同時ストリーム(スロット数厳守: bp §8)."""

    async def _run() -> dict[str, Any]:
        prompt = bench_prompt()
        async with httpx.AsyncClient(
            base_url=base_url, timeout=httpx.Timeout(600.0, connect=10.0)
        ) as http:
            await _one_stream(http, prompt, 16)  # warmup
            started = time.monotonic()
            results = await asyncio.gather(
                *(_one_stream(http, prompt, n_predict) for _ in range(np))
            )
            wall_s = time.monotonic() - started
        per_stream = [round(float(r["timings"]["predicted_per_second"]), 1) for r in results]
        total_tokens = sum(int(r["timings"]["predicted_n"]) for r in results)
        return {
            "model_key": spec.key,
            "np": np,
            "per_stream_tg_tps": per_stream,
            "aggregate_tg_tps": round(total_tokens / wall_s, 1),
            "wall_s": round(wall_s, 1),
        }

    return asyncio.run(_run())
