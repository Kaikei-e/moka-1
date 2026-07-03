"""judge.extract_verdict — 自由記述CoT+[[N]]タグ方式(json_schema縛りは推論10-15%劣化)."""

from moka_eval.judge import extract_verdict


def test_extracts_simple_tag() -> None:
    assert extract_verdict("Aの方が忠実。判定: [[A]]") == "A"
    assert extract_verdict("Bが網羅的。判定: [[B]]") == "B"
    assert extract_verdict("甲乙つけがたい。判定: [[tie]]") == "tie"


def test_uses_last_tag_when_multiple() -> None:
    # CoT中に例示として [[A]] が出ても、最終判定(最後のタグ)を採る
    text = "仮に[[A]]とすると…いや、やはり網羅性でBが上回る。判定: [[B]]"
    assert extract_verdict(text) == "B"


def test_returns_none_when_no_tag() -> None:
    assert extract_verdict("どちらも良い要約です。") is None


def test_tolerates_fullwidth_and_case() -> None:
    assert extract_verdict("判定: [[TIE]]") == "tie"
    assert extract_verdict("判定:[[a]]") == "A"
