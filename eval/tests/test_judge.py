"""judge — ブラインド化ペア生成と位置スワップ集計."""

from moka_eval.judge import decide_pair, make_pairs


def _gen(model: str, article: str, answer: str) -> dict[str, str]:
    return {"model_key": model, "article_id": article, "answer": answer}


def test_make_pairs_is_deterministic_given_seed() -> None:
    gen_x = [_gen("model-x", f"art-{i}", f"x{i}") for i in range(6)]
    gen_y = [_gen("model-y", f"art-{i}", f"y{i}") for i in range(6)]
    pairs1, key1 = make_pairs(gen_x, gen_y, seed=7)
    pairs2, key2 = make_pairs(gen_x, gen_y, seed=7)
    assert [p.pair_id for p in pairs1] == [p.pair_id for p in pairs2]
    assert [p.text_a for p in pairs1] == [p.text_a for p in pairs2]
    assert key1 == key2


def test_make_pairs_randomizes_sides() -> None:
    gen_x = [_gen("model-x", f"art-{i}", f"x{i}") for i in range(20)]
    gen_y = [_gen("model-y", f"art-{i}", f"y{i}") for i in range(20)]
    _, key = make_pairs(gen_x, gen_y, seed=7)
    sides_for_x = {v["A"] for v in key.values()}
    # 20ペアあれば model-x が A側にも B側にも現れるはず(ブラインド化の要)
    assert sides_for_x == {"model-x", "model-y"}


def test_make_pairs_key_maps_back_to_models() -> None:
    gen_x = [_gen("model-x", "art-0", "xxx")]
    gen_y = [_gen("model-y", "art-0", "yyy")]
    pairs, key = make_pairs(gen_x, gen_y, seed=1)
    pair = pairs[0]
    mapping = key[pair.pair_id]
    text_of = {"model-x": "xxx", "model-y": "yyy"}
    assert pair.text_a == text_of[mapping["A"]]
    assert pair.text_b == text_of[mapping["B"]]


def test_make_pairs_skips_articles_missing_on_either_side() -> None:
    gen_x = [_gen("model-x", "art-0", "x"), _gen("model-x", "art-1", "x")]
    gen_y = [_gen("model-y", "art-0", "y")]
    pairs, _ = make_pairs(gen_x, gen_y, seed=1)
    assert len(pairs) == 1


def test_decide_pair_consistent_win_original_orientation() -> None:
    # 原順で A 勝ち、スワップ順(A/B入替)で B 勝ち → 同一実体が両順で勝ち → "A"
    assert decide_pair("A", "B") == "A"
    assert decide_pair("B", "A") == "B"


def test_decide_pair_position_inconsistency_is_tie() -> None:
    # 両順とも "A" を選ぶ = 位置バイアス → tie 扱い(両順一致のみ勝ち)
    assert decide_pair("A", "A") == "tie"
    assert decide_pair("B", "B") == "tie"


def test_decide_pair_any_tie_is_tie() -> None:
    assert decide_pair("tie", "B") == "tie"
    assert decide_pair("A", "tie") == "tie"
    assert decide_pair("tie", "tie") == "tie"
