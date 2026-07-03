"""report — 符号検定と判定集計(bp-python §12: paired comparison)."""

import math

from moka_eval.report import sign_test_p


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
