"""再開時の二重記録防止 — 記録済みユニットのスキップ(judge / generate)."""

from pathlib import Path

import pytest

from moka_eval import generate, judge
from moka_eval.config import Sampling
from moka_eval.records import BlindPair, GenerationRecord, VerdictRecord


def _verdict(pair_id: str, lens: str) -> VerdictRecord:
    return VerdictRecord(
        pair_id=pair_id,
        judge_key="judge-x",
        lens=lens,
        winner_orig="A",
        winner_swap="B",
        verdict="A",
        analysis_orig="",
        analysis_swap="",
        prompt_sha256="deadbeef",
        created_at="2026-07-06T00:00:00+00:00",
    )


def _pair(pair_id: str) -> BlindPair:
    return BlindPair(pair_id=pair_id, article_id="art", task="summarize", text_a="a", text_b="b")


def _generation(task: str, article_id: str, seed: int) -> GenerationRecord:
    return GenerationRecord(
        run_id="run-1",
        model_key="model-x",
        model_hf="x/y",
        llamacpp_build="b0",
        task=task,
        article_id=article_id,
        seed=seed,
        sampling=Sampling(temperature=0.0),
        prompt_sha256="deadbeef",
        server_flags=[],
        ttft_ms=1.0,
        total_ms=2.0,
        prompt_tokens=1,
        predicted_tokens=1,
        cot_tokens=0,
        answer_tokens=1,
        pp_tps_derived=None,
        tg_tps_derived=None,
        effective_answer_tps=1.0,
        timings=None,
        cot=None,
        answer="x",
        finish_reason="stop",
        created_at="2026-07-06T00:00:00+00:00",
    )


def test_judge_completed_units_empty_when_missing(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(judge, "RESULTS_DIR", tmp_path)
    assert judge.completed_units("dec", "judge-x") == set()


def test_judge_pending_units_skips_recorded(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(judge, "RESULTS_DIR", tmp_path)
    judge.append_verdict("dec", "judge-x", _verdict("summarize:art-0", "faithfulness"))
    judge.append_verdict("dec", "judge-x", _verdict("summarize:art-0", "coverage"))
    done = judge.completed_units("dec", "judge-x")
    assert done == {("summarize:art-0", "faithfulness"), ("summarize:art-0", "coverage")}
    pairs = [_pair("summarize:art-0"), _pair("summarize:art-1")]
    todo = judge.pending_units(pairs, ["faithfulness", "coverage"], done)
    assert [(p.pair_id, lens) for p, lens in todo] == [
        ("summarize:art-1", "faithfulness"),
        ("summarize:art-1", "coverage"),
    ]


def test_judge_pending_units_no_duplicates_after_resume(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    # 途中クラッシュ相当の部分ファイル → 再開しても各ユニットは1回しか実行されない
    monkeypatch.setattr(judge, "RESULTS_DIR", tmp_path)
    judge.append_verdict("dec", "judge-x", _verdict("summarize:art-0", "faithfulness"))
    pairs = [_pair("summarize:art-0"), _pair("summarize:art-1")]
    lenses = ["faithfulness"]
    todo = judge.pending_units(pairs, lenses, judge.completed_units("dec", "judge-x"))
    for pair, lens in todo:
        judge.append_verdict("dec", "judge-x", _verdict(pair.pair_id, lens))
    units = [(v.pair_id, v.lens) for v in judge.load_verdicts("dec", "judge-x")]
    assert len(units) == len(set(units)) == 2


def test_generate_completed_units_empty_when_missing(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(generate, "RESULTS_DIR", tmp_path)
    assert generate.completed_units("run-1") == set()


def test_generate_pending_items_skips_recorded(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(generate, "RESULTS_DIR", tmp_path)
    generate.append_record("run-1", _generation("summarize", "art-0", 42))
    generate.append_record("run-1", _generation("summarize", "art-1", 42))
    done = generate.completed_units("run-1")
    assert done == {("summarize", "art-0", 42), ("summarize", "art-1", 42)}
    items = [("art-0", "p0"), ("art-1", "p1")]
    todo = generate.pending_items(items, [42, 43], task="summarize", done=done)
    assert [(seed, item_id) for seed, item_id, _ in todo] == [(43, "art-0"), (43, "art-1")]


def test_generate_pending_items_distinguishes_task(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr(generate, "RESULTS_DIR", tmp_path)
    generate.append_record("run-1", _generation("tags", "art-0", 42))
    todo = generate.pending_items(
        [("art-0", "p0")], [42], task="summarize", done=generate.completed_units("run-1")
    )
    assert [(seed, item_id) for seed, item_id, _ in todo] == [(42, "art-0")]
