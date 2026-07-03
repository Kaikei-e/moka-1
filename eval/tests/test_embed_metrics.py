"""embed — retrieval メトリクス(recall@k / MRR)と MRL 次元切り詰め."""

import math

from moka_eval.embed import cosine, mrr, rank_of_gold, recall_at_k, truncate_normalize


def test_cosine_identical_is_one() -> None:
    assert math.isclose(cosine([1.0, 2.0, 3.0], [1.0, 2.0, 3.0]), 1.0)


def test_cosine_orthogonal_is_zero() -> None:
    assert math.isclose(cosine([1.0, 0.0], [0.0, 1.0]), 0.0)


def test_rank_of_gold() -> None:
    ranked = ["art-2", "art-0", "art-1"]
    assert rank_of_gold(ranked, "art-0") == 2
    assert rank_of_gold(ranked, "art-9") is None


def test_recall_at_k() -> None:
    ranks = [1, 2, 5, None]  # 4クエリ分のgold順位
    assert math.isclose(recall_at_k(ranks, 1), 0.25)
    assert math.isclose(recall_at_k(ranks, 3), 0.5)


def test_mrr() -> None:
    ranks = [1, 2, None, 4]
    expected = (1.0 + 0.5 + 0.0 + 0.25) / 4
    assert math.isclose(mrr(ranks), expected)


def test_truncate_normalize_produces_unit_vector() -> None:
    vec = [3.0, 4.0, 100.0, 100.0]
    out = truncate_normalize(vec, 2)
    assert len(out) == 2
    assert math.isclose(math.hypot(*out), 1.0)
    assert math.isclose(out[0], 0.6)
    assert math.isclose(out[1], 0.8)
