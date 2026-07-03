"""生成マトリクス実行 — (モデル × タスク × 記事 × seed) → generations.jsonl."""

import hashlib
import json
from collections.abc import Sequence
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from pydantic import BaseModel, Field, ValidationError

from moka_eval.client import LlamaClient
from moka_eval.config import LLAMACPP_BUILD, PROMPTS_DIR, RESULTS_DIR, ModelSpec
from moka_eval.dataset import Article, RecapWeek
from moka_eval.records import GenerationRecord


class TagResult(BaseModel):
    """タグ抽出の構造化出力(スキーマと検証の一元化: bp-python §7)."""

    tags: list[str] = Field(min_length=1, max_length=5)


class GenerateError(RuntimeError):
    """生成実行の失敗."""


def _now_iso() -> str:
    return datetime.now(UTC).isoformat(timespec="seconds")


def load_prompt(task: str, override_path: Path | None = None) -> tuple[str, str]:
    """(テンプレート, sha256) を返す。override_path はプロンプトA/B比較用の差し替え."""
    path = override_path or PROMPTS_DIR / f"{task}_ja.txt"
    if not path.is_file():
        msg = f"prompt template missing: {path}"
        raise GenerateError(msg)
    template = path.read_text(encoding="utf-8")
    return template, hashlib.sha256(template.encode()).hexdigest()


def render_article_prompt(template: str, article: Article) -> str:
    return template.replace("<<TITLE>>", article.title).replace("<<TEXT>>", article.text)


def render_recap_prompt(template: str, week: RecapWeek) -> str:
    lines = [f"- {item.title}: {item.summary}" for item in week.entries]
    return template.replace("<<ENTRIES>>", "\n".join(lines))


def _derived_tps(
    ttft_ms: float, total_ms: float, prompt_tokens: int, predicted_tokens: int
) -> tuple[float | None, float | None]:
    pp = prompt_tokens / (ttft_ms / 1000.0) if ttft_ms > 0 and prompt_tokens > 0 else None
    decode_ms = total_ms - ttft_ms
    tg = (
        (predicted_tokens - 1) / (decode_ms / 1000.0)
        if decode_ms > 0 and predicted_tokens > 1
        else None
    )
    return pp, tg


def generate_one(
    client: LlamaClient,
    spec: ModelSpec,
    *,
    run_id: str,
    task: str,
    item_id: str,
    prompt: str,
    prompt_sha: str,
    seed: int,
    max_tokens: int,
    server_flags: Sequence[str],
    json_schema: dict[str, Any] | None = None,
    model_key: str | None = None,
) -> GenerationRecord:
    result = client.chat(
        prompt,
        sampling=spec.sampling,
        seed=seed,
        max_tokens=max_tokens,
        json_schema=json_schema,
        chat_template_kwargs=spec.chat_kwargs,
    )
    cot_tokens = client.tokenize_count(result.cot) if result.cot else 0
    answer_tokens = client.tokenize_count(result.answer) if result.answer else 0
    pp_tps, tg_tps = _derived_tps(
        result.ttft_ms, result.total_ms, result.prompt_tokens, result.predicted_tokens
    )
    parse_ok: bool | None = None
    if json_schema is not None:
        try:
            TagResult.model_validate_json(result.answer)
            parse_ok = True
        except ValidationError:
            parse_ok = False
    effective = answer_tokens / (result.total_ms / 1000.0) if result.total_ms > 0 else 0.0
    return GenerationRecord(
        run_id=run_id,
        model_key=model_key or spec.key,
        model_hf=spec.hf,
        llamacpp_build=LLAMACPP_BUILD,
        task=task,
        article_id=item_id,
        seed=seed,
        sampling=spec.sampling,
        prompt_sha256=prompt_sha,
        server_flags=list(server_flags),
        ttft_ms=round(result.ttft_ms, 1),
        total_ms=round(result.total_ms, 1),
        prompt_tokens=result.prompt_tokens,
        predicted_tokens=result.predicted_tokens,
        cot_tokens=cot_tokens,
        answer_tokens=answer_tokens,
        pp_tps_derived=round(pp_tps, 1) if pp_tps is not None else None,
        tg_tps_derived=round(tg_tps, 1) if tg_tps is not None else None,
        effective_answer_tps=round(effective, 2),
        timings=result.timings,
        cot=result.cot,
        answer=result.answer,
        finish_reason=result.finish_reason,
        parse_ok=parse_ok,
        created_at=_now_iso(),
    )


def run_dir(run_id: str) -> Path:
    return RESULTS_DIR / run_id


def append_record(run_id: str, record: GenerationRecord) -> None:
    directory = run_dir(run_id)
    directory.mkdir(parents=True, exist_ok=True)
    with (directory / "generations.jsonl").open("a", encoding="utf-8") as f:
        f.write(record.model_dump_json() + "\n")


def write_run_meta(run_id: str, meta: dict[str, Any]) -> None:
    directory = run_dir(run_id)
    directory.mkdir(parents=True, exist_ok=True)
    (directory / "run.json").write_text(
        json.dumps(meta, ensure_ascii=False, indent=2), encoding="utf-8"
    )


def load_generations(run_id: str) -> list[GenerationRecord]:
    path = run_dir(run_id) / "generations.jsonl"
    if not path.is_file():
        msg = f"generations not found: {path}"
        raise GenerateError(msg)
    with path.open(encoding="utf-8") as f:
        return [GenerationRecord.model_validate_json(line) for line in f if line.strip()]
