"""ローカルMoA審判 — ブラインド化ペア + 位置スワップ + 観点レンズ.

プロトコルは Phase R 調査(eval/results/research/local-judge.json ほか)に基づく:
- 主審は候補と別族の最強単一審判(gpt-oss-20b)。弱い異種審判の多数決は組まない
  (エラー相関で実効票が増えない: arXiv 2605.29800)。10B未満は審判にしない
- 位置スワップ両順で同一実体が勝った時のみ勝ち、不一致・tie宣言は tie
  (inconsistency-as-a-tie: arXiv 2406.07791)
- 判定は自由記述CoT→末尾 `[[A]]/[[B]]/[[tie]]` タグ抽出。json_schema で生成全体を
  縛らない(推論10-15%劣化の報告)。パース失敗は max_tokens 倍増で1回リトライ、
  なお失敗なら tie
- 判定は greedy(temperature 0)。同一審判の温度サンプリング多数決は使わない
- 「日本語の自然さ」レンズは使わない(人手相関0.48で飽和)。代わりに簡潔さ
"""

import hashlib
import json
import random
import re
from collections.abc import Mapping, Sequence
from datetime import UTC, datetime
from pathlib import Path

from moka_eval.client import LlamaClient
from moka_eval.config import GREEDY, PROMPTS_DIR, RESULTS_DIR, ModelSpec
from moka_eval.records import BlindPair, VerdictRecord

type PairKey = dict[str, dict[str, str]]

LENSES: dict[str, str] = {
    "faithfulness": (
        "忠実性 — 原文に書かれていない情報の捏造や事実の歪曲がないか。捏造・歪曲が少ない方が勝ち。"
    ),
    "coverage": "網羅性 — 原文の主要な論点・結論をどれだけ落とさず含めているか。",
    "conciseness": "簡潔さ — 主要な論点に対応しない余剰・冗長な記述がどれだけ少ないか。",
    "instruction": "指示遵守 — 出力形式・分量・言語(日本語)の指示にどれだけ従えているか。",
}

_VERDICT_RE = re.compile(r"\[\[\s*(a|b|tie)\s*\]\]", re.IGNORECASE)


class JudgeError(RuntimeError):
    """審判実行の失敗."""


def extract_verdict(text: str) -> str | None:
    """自由記述から最後の [[A]]/[[B]]/[[tie]] タグを抽出する。なければ None."""
    matches = _VERDICT_RE.findall(text)
    if not matches:
        return None
    last = matches[-1].lower()
    return "tie" if last == "tie" else last.upper()


def _duplicate_ids(gens: Sequence[Mapping[str, str]]) -> set[str]:
    seen: set[str] = set()
    duplicates: set[str] = set()
    for g in gens:
        article_id = g["article_id"]
        if article_id in seen:
            duplicates.add(article_id)
        seen.add(article_id)
    return duplicates


def make_pairs(
    gen_a: Sequence[Mapping[str, str]],
    gen_b: Sequence[Mapping[str, str]],
    *,
    seed: int,
    task: str = "summarize",
) -> tuple[list[BlindPair], PairKey]:
    """同一 article_id の出力をブラインド化ペアにする。左右はseed付きRNGで無作為化.

    article_id が重複すると pair_id が衝突し key が上書きされ判定の帰属が壊れるため、
    重複は ValueError にする。
    """
    duplicates = sorted(_duplicate_ids(gen_a) | _duplicate_ids(gen_b))
    if duplicates:
        msg = f"duplicate article_id(s) in generations: {', '.join(duplicates)}"
        raise ValueError(msg)
    rng = random.Random(seed)
    by_article_b = {g["article_id"]: g for g in gen_b}
    pairs: list[BlindPair] = []
    key: PairKey = {}
    for record_a in gen_a:
        article_id = record_a["article_id"]
        record_b = by_article_b.get(article_id)
        if record_b is None:
            continue
        pair_id = f"{task}:{article_id}"
        if rng.random() < 0.5:
            first, second = record_a, record_b
        else:
            first, second = record_b, record_a
        pairs.append(
            BlindPair(
                pair_id=pair_id,
                article_id=article_id,
                task=task,
                text_a=first["answer"],
                text_b=second["answer"],
            )
        )
        key[pair_id] = {"A": first["model_key"], "B": second["model_key"]}
    return pairs, key


def decide_pair(winner_orig: str, winner_swap: str) -> str:
    """位置スワップ集計: 両順で同一実体が勝った時のみ勝ち(原順ラベルで返す)."""
    if winner_orig == "A" and winner_swap == "B":
        return "A"
    if winner_orig == "B" and winner_swap == "A":
        return "B"
    return "tie"


def load_judge_prompt() -> tuple[str, str]:
    path = PROMPTS_DIR / "judge_pairwise_ja.txt"
    template = path.read_text(encoding="utf-8")
    return template, hashlib.sha256(template.encode()).hexdigest()


def render_judge_prompt(template: str, *, lens: str, source: str, text_a: str, text_b: str) -> str:
    return (
        template.replace("<<LENS_RUBRIC>>", LENSES[lens])
        .replace("<<SOURCE>>", source)
        .replace("<<A>>", text_a)
        .replace("<<B>>", text_b)
    )


def _judge_once(
    client: LlamaClient,
    judge_spec: ModelSpec,
    prompt: str,
    *,
    max_tokens: int,
) -> str:
    """1判定。タグ抽出失敗は max_tokens を倍にして1回リトライ、なお失敗なら 'parse_fail'.

    greedy + 固定 seed のため同一条件の再送は決定的に同じ出力になる。失敗の主因は
    打ち切り(truncation)なので、リトライは max_tokens の倍増のみ行う(判定
    プロトコル = greedy は維持)。CoT 途中打ち切り(finish_reason=length かつ
    think後の回答が空)は未完 CoT 中の投機タグを拾わずパース失敗として扱う。
    """
    for attempt in range(2):
        result = client.chat(prompt, sampling=GREEDY, seed=42, max_tokens=max_tokens * 2**attempt)
        if result.finish_reason == "length" and not result.answer:
            continue
        verdict = extract_verdict(result.answer) or extract_verdict(result.content)
        if verdict is not None:
            return verdict
    return "parse_fail"


def judge_pair(
    client: LlamaClient,
    judge_spec: ModelSpec,
    pair: BlindPair,
    *,
    lens: str,
    source: str,
    template: str,
    prompt_sha: str,
    max_tokens: int = 2048,
) -> VerdictRecord:
    """1ペア×1レンズを両順で判定して集計する。parse_fail は tie 扱いで記録に残す."""
    winners: list[str] = []
    for text_a, text_b in ((pair.text_a, pair.text_b), (pair.text_b, pair.text_a)):
        prompt = render_judge_prompt(
            template, lens=lens, source=source, text_a=text_a, text_b=text_b
        )
        winners.append(_judge_once(client, judge_spec, prompt, max_tokens=max_tokens))
    winner_orig, winner_swap = winners
    return VerdictRecord(
        pair_id=pair.pair_id,
        judge_key=judge_spec.key,
        lens=lens,
        winner_orig=winner_orig,
        winner_swap=winner_swap,
        verdict=decide_pair(winner_orig, winner_swap),
        analysis_orig="",
        analysis_swap="",
        prompt_sha256=prompt_sha,
        created_at=datetime.now(UTC).isoformat(timespec="seconds"),
    )


def pairs_path(name: str) -> Path:
    return RESULTS_DIR / "pairs" / f"{name}.json"


def key_path(name: str) -> Path:
    return RESULTS_DIR / "pairs" / f"{name}.key.json"


def verdicts_path(name: str, judge_key: str) -> Path:
    return RESULTS_DIR / "judgments" / f"{name}.{judge_key}.verdicts.jsonl"


def save_pairs(name: str, pairs: list[BlindPair], key: PairKey) -> None:
    directory = RESULTS_DIR / "pairs"
    directory.mkdir(parents=True, exist_ok=True)
    pairs_path(name).write_text(
        json.dumps([p.model_dump() for p in pairs], ensure_ascii=False, indent=2),
        encoding="utf-8",
    )
    key_path(name).write_text(json.dumps(key, ensure_ascii=False, indent=2), encoding="utf-8")


def load_pairs(name: str) -> tuple[list[BlindPair], PairKey]:
    pairs = [
        BlindPair.model_validate(p)
        for p in json.loads(pairs_path(name).read_text(encoding="utf-8"))
    ]
    key: PairKey = json.loads(key_path(name).read_text(encoding="utf-8"))
    return pairs, key


def append_verdict(name: str, judge_key: str, record: VerdictRecord) -> None:
    path = verdicts_path(name, judge_key)
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as f:
        f.write(record.model_dump_json() + "\n")


def completed_units(name: str, judge_key: str) -> set[tuple[str, str]]:
    """再開用: 記録済み (pair_id, lens) の集合。ファイル無しは空集合."""
    path = verdicts_path(name, judge_key)
    if not path.is_file():
        return set()
    with path.open(encoding="utf-8") as f:
        return {
            (r.pair_id, r.lens)
            for r in (VerdictRecord.model_validate_json(line) for line in f if line.strip())
        }


def pending_units(
    pairs: Sequence[BlindPair], lenses: Sequence[str], done: set[tuple[str, str]]
) -> list[tuple[BlindPair, str]]:
    """未実行の (pair, lens) を列挙する(再実行時の二重投票防止)."""
    return [(p, lens) for p in pairs for lens in lenses if (p.pair_id, lens) not in done]


def load_verdicts(name: str, judge_key: str) -> list[VerdictRecord]:
    path = verdicts_path(name, judge_key)
    if not path.is_file():
        msg = f"verdicts not found: {path}"
        raise JudgeError(msg)
    with path.open(encoding="utf-8") as f:
        return [VerdictRecord.model_validate_json(line) for line in f if line.strip()]
