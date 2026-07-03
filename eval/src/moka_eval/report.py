"""集計 — 選好率・両側符号検定・位置一貫性・審判間一致率・レイテンシ要約."""

import json
import math
import statistics
from datetime import UTC, datetime
from typing import Any

from moka_eval.config import RESULTS_DIR
from moka_eval.judge import PairKey
from moka_eval.records import GenerationRecord, VerdictRecord


def sign_test_p(wins_a: int, wins_b: int) -> float:
    """両側符号検定(tie は呼び出し側で除外済み)。n=0 は判定不能 → 1.0."""
    n = wins_a + wins_b
    if n == 0:
        return 1.0
    k = max(wins_a, wins_b)
    tail = sum(math.comb(n, i) for i in range(k, n + 1)) / 2**n
    return min(1.0, 2.0 * tail)


def _unblind(verdict: str, mapping: dict[str, str]) -> str:
    """A/B 判定をモデルキーに戻す。tie はそのまま."""
    if verdict in mapping:
        return mapping[verdict]
    return "tie"


def pair_level_winners(verdicts: list[VerdictRecord], key: PairKey) -> dict[str, str]:
    """ペアごとに全(審判×レンズ)票を多数決してモデルキー or 'tie' を返す."""
    votes: dict[str, list[str]] = {}
    for record in verdicts:
        model_or_tie = _unblind(record.verdict, key[record.pair_id])
        votes.setdefault(record.pair_id, []).append(model_or_tie)
    winners: dict[str, str] = {}
    for pair_id, pair_votes in votes.items():
        counts: dict[str, int] = {}
        for vote in pair_votes:
            if vote != "tie":
                counts[vote] = counts.get(vote, 0) + 1
        if not counts:
            winners[pair_id] = "tie"
            continue
        ranked = sorted(counts.items(), key=lambda kv: kv[1], reverse=True)
        if len(ranked) > 1 and ranked[0][1] == ranked[1][1]:
            winners[pair_id] = "tie"
        else:
            winners[pair_id] = ranked[0][0]
    return winners


def position_consistency(verdicts: list[VerdictRecord]) -> float:
    """両順で同一実体を選べた割合(位置バイアスの逆指標)."""
    if not verdicts:
        return 0.0
    consistent = sum(1 for v in verdicts if v.verdict != "tie" or v.winner_orig == "tie")
    return consistent / len(verdicts)


def judge_agreement(
    verdicts_by_judge: dict[str, list[VerdictRecord]], key: PairKey
) -> float | None:
    """審判間のペアレベル一致率(2審判以上のとき)."""
    if len(verdicts_by_judge) < 2:
        return None
    winner_maps = [pair_level_winners(v, key) for v in verdicts_by_judge.values()]
    common = set.intersection(*(set(m) for m in winner_maps))
    if not common:
        return None
    agree = sum(1 for pid in common if len({m[pid] for m in winner_maps}) == 1)
    return agree / len(common)


def score_decision(
    name: str,
    verdicts_by_judge: dict[str, list[VerdictRecord]],
    key: PairKey,
) -> dict[str, Any]:
    """1決定分の集計サマリを作って results/summary/ に書く."""
    all_verdicts = [v for vs in verdicts_by_judge.values() for v in vs]
    winners = pair_level_winners(all_verdicts, key)
    models = sorted({m for mapping in key.values() for m in mapping.values()})
    win_counts = {m: sum(1 for w in winners.values() if w == m) for m in models}
    ties = sum(1 for w in winners.values() if w == "tie")
    per_lens: dict[str, dict[str, int]] = {}
    for record in all_verdicts:
        lens_counts = per_lens.setdefault(record.lens, dict.fromkeys([*models, "tie"], 0))
        lens_counts[_unblind(record.verdict, key[record.pair_id])] += 1
    wins = sorted(win_counts.items(), key=lambda kv: kv[1], reverse=True)
    p_value = sign_test_p(wins[0][1], wins[1][1]) if len(wins) >= 2 else 1.0
    summary: dict[str, Any] = {
        "decision": name,
        "n_pairs": len(winners),
        "pair_level_wins": win_counts,
        "ties": ties,
        "sign_test_p": round(p_value, 5),
        "winner": wins[0][0] if len(wins) >= 2 and wins[0][1] > wins[1][1] else "tie",
        "per_lens": per_lens,
        "position_consistency": round(position_consistency(all_verdicts), 3),
        "judge_agreement": judge_agreement(verdicts_by_judge, key),
        "judges": sorted(verdicts_by_judge),
        "created_at": datetime.now(UTC).isoformat(timespec="seconds"),
    }
    out_dir = RESULTS_DIR / "summary"
    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / f"{name}.json").write_text(
        json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    return summary


def latency_summary(records: list[GenerationRecord]) -> dict[str, Any]:
    """1 run 分のレイテンシ・CoT統計(bp-python §13: pp/tg 分離報告)."""
    if not records:
        return {}
    total_s = [r.total_ms / 1000.0 for r in records]
    cot_share = [r.cot_tokens / r.predicted_tokens for r in records if r.predicted_tokens > 0]
    return {
        "model_key": records[0].model_key,
        "task": records[0].task,
        "n": len(records),
        "total_s_median": round(statistics.median(total_s), 2),
        "total_s_mean": round(statistics.fmean(total_s), 2),
        "ttft_ms_median": round(statistics.median(r.ttft_ms for r in records), 1),
        "effective_answer_tps_median": round(
            statistics.median(r.effective_answer_tps for r in records), 2
        ),
        "cot_tokens_median": statistics.median(r.cot_tokens for r in records),
        "cot_share_mean": round(statistics.fmean(cot_share), 3) if cot_share else 0.0,
        "answer_tokens_median": statistics.median(r.answer_tokens for r in records),
        "truncated": sum(1 for r in records if r.finish_reason == "length"),
    }
