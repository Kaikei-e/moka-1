"""client.split_think — CoT と最終回答の分離(bp-python §10)."""

from moka_eval.client import split_think


def test_splits_cot_and_answer() -> None:
    cot, answer = split_think("<think>英語のCoTが出る</think>\n要約本文です。")
    assert cot == "英語のCoTが出る"
    assert answer == "要約本文です。"


def test_no_think_tag_returns_full_text_as_answer() -> None:
    cot, answer = split_think("タグなしの平文出力。")
    assert cot is None
    assert answer == "タグなしの平文出力。"


def test_splits_at_last_close_tag() -> None:
    text = "<think>a</think>中間<think>b</think>最終回答"
    cot, answer = split_think(text)
    assert answer == "最終回答"
    assert cot is not None
    assert "b" in cot


def test_unclosed_think_treats_all_as_cot() -> None:
    # 生成打ち切りで </think> が出なかった場合: 回答なし扱い
    cot, answer = split_think("<think>途中で切れた")
    assert cot == "途中で切れた"
    assert answer == ""


def test_missing_open_tag_still_splits() -> None:
    # llama.cpp が開始タグを吸収して閉じタグだけ残るケース(LFM2.5で既知)
    cot, answer = split_think("勝手に始まるCoT</think>回答")
    assert cot == "勝手に始まるCoT"
    assert answer == "回答"
