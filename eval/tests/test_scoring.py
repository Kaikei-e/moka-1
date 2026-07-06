"""report — 符号検定と判定集計(bp-python §12: paired comparison)."""

import math

from moka_eval.judge import decide_pair
from moka_eval.records import VerdictRecord
from moka_eval.report import position_consistency, sign_test_p


def _verdict(winner_orig: str, winner_swap: str) -> VerdictRecord:
    return VerdictRecord(
        pair_id="summarize:art-0",
        judge_key="judge-x",
        lens="faithfulness",
        winner_orig=winner_orig,
        winner_swap=winner_swap,
        verdict=decide_pair(winner_orig, winner_swap),
        analysis_orig="",
        analysis_swap="",
        prompt_sha256="deadbeef",
        created_at="2026-07-06T00:00:00+00:00",
    )


def test_sign_test_known_value() -> None:
    # n=10, 8勝2敗: 両側 p = 2 * (C(10,8)+C(10,9)+C(10,10)) / 2^10 = 0.109375
    assert math.isclose(sign_test_p(8, 2), 0.109375)


def test_sign_test_even_split_is_one() -> None:
    assert math.isclose(sign_test_p(5, 5), 1.0)


def test_sign_test_caps_at_one() -> None:
    assert sign_test_p(6, 5) <= 1.0


def test_sign_test_all_ties_returns_one() -> None:
    # tie は除外済みの引数設計: 0勝0敗は判定不能 → p=1.0
    assert sign_test_p(0, 0) == 1.0


def test_sign_test_strong_preference_significant() -> None:
    # 25勝5敗 (N=30想定でtie除外後) は p < 0.05
    assert sign_test_p(25, 5) < 0.05


def test_position_consistency_tie_then_entity_is_inconsistent() -> None:
    # 片順だけ tie は「両順で同一実体」ではない(対称に扱う)
    assert position_consistency([_verdict("tie", "A")]) == 0.0
    assert position_consistency([_verdict("A", "tie")]) == 0.0


def test_position_consistency_double_tie_is_consistent() -> None:
    assert position_consistency([_verdict("tie", "tie")]) == 1.0


def test_position_consistency_entity_agreement_is_consistent() -> None:
    # 原順A勝ち+スワップ順B勝ち = 同一実体(いずれの向きでも)
    assert position_consistency([_verdict("A", "B")]) == 1.0
    assert position_consistency([_verdict("B", "A")]) == 1.0


def test_position_consistency_excludes_parse_fail() -> None:
    # parse_fail は位置バイアスについて何も語らない → 分子・分母とも除外
    verdicts = [_verdict("parse_fail", "A"), _verdict("A", "B")]
    assert position_consistency(verdicts) == 1.0


def test_position_consistency_all_parse_fail_returns_zero() -> None:
    assert position_consistency([_verdict("A", "parse_fail")]) == 0.0
    assert position_consistency([]) == 0.0
