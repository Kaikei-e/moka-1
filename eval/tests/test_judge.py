"""judge — ブラインド化ペア生成と位置スワップ集計."""

import pytest

from moka_eval.client import ChatResult
from moka_eval.config import GREEDY, ModelSpec, Sampling
from moka_eval.judge import _judge_once, decide_pair, make_pairs


def _gen(model: str, article: str, answer: str) -> dict[str, str]:
    return {"model_key": model, "article_id": article, "answer": answer}


def _chat_result(
    content: str, answer: str, *, finish_reason: str = "stop", cot: str | None = None
) -> ChatResult:
    return ChatResult(
        content=content,
        cot=cot,
        answer=answer,
        ttft_ms=1.0,
        total_ms=2.0,
        prompt_tokens=10,
        predicted_tokens=10,
        finish_reason=finish_reason,
        timings=None,
    )


class _FakeClient:
    """chat 呼び出しを記録し、用意した結果を順に返すスタブ."""

    def __init__(self, results: list[ChatResult]) -> None:
        self._results = list(results)
        self.max_tokens_calls: list[int] = []

    def chat(self, prompt: str, *, sampling: Sampling, seed: int, max_tokens: int) -> ChatResult:
        self.max_tokens_calls.append(max_tokens)
        return self._results.pop(0)


_JUDGE_SPEC = ModelSpec(key="judge-x", hf="x/y", sampling=GREEDY)


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


def test_make_pairs_raises_on_duplicate_article_ids() -> None:
    # 同一 article_id が重複すると pair_id が衝突し key が最後の対応で上書きされる
    gen_x = [_gen("model-x", "art-0", "x1"), _gen("model-x", "art-0", "x2")]
    gen_y = [_gen("model-y", "art-0", "y")]
    with pytest.raises(ValueError, match="art-0"):
        make_pairs(gen_x, gen_y, seed=1)


def test_judge_once_retry_doubles_max_tokens() -> None:
    # greedy+固定seedの同一再送は無意味 → リトライは max_tokens 倍増で打ち切りを解消する
    client = _FakeClient(
        [
            _chat_result("タグなしの判定文。", "タグなしの判定文。"),
            _chat_result("判定: [[A]]", "判定: [[A]]"),
        ]
    )
    verdict = _judge_once(client, _JUDGE_SPEC, "p", max_tokens=512)  # type: ignore[arg-type]
    assert verdict == "A"
    assert client.max_tokens_calls == [512, 1024]


def test_judge_once_truncated_cot_does_not_scoop_tags() -> None:
    # CoT途中打ち切り(finish_reason=length・回答空)の未完CoTから投機タグを拾わない
    truncated = _chat_result(
        "<think>仮に[[B]]とすると…", "", finish_reason="length", cot="仮に[[B]]とすると…"
    )
    client = _FakeClient([truncated, truncated.model_copy()])
    verdict = _judge_once(client, _JUDGE_SPEC, "p", max_tokens=512)  # type: ignore[arg-type]
    assert verdict == "parse_fail"
    # 打ち切りはE-2リトライ(max_tokens倍増)を起こすこと
    assert client.max_tokens_calls == [512, 1024]


def test_judge_once_truncated_then_retry_succeeds() -> None:
    client = _FakeClient(
        [
            _chat_result("<think>思考中…", "", finish_reason="length", cot="思考中…"),
            _chat_result("判定: [[B]]", "判定: [[B]]"),
        ]
    )
    verdict = _judge_once(client, _JUDGE_SPEC, "p", max_tokens=512)  # type: ignore[arg-type]
    assert verdict == "B"


def test_judge_once_keeps_content_fallback_when_not_truncated() -> None:
    # 非打ち切りで回答部が空でも content 側のタグは拾う(thinkタグ内で答える個体対策)
    client = _FakeClient(
        [_chat_result("<think>網羅性でA。判定: [[A]]</think>", "", cot="網羅性でA。判定: [[A]]")]
    )
    verdict = _judge_once(client, _JUDGE_SPEC, "p", max_tokens=512)  # type: ignore[arg-type]
    assert verdict == "A"
