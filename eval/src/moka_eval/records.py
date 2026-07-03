"""結果JSONLの行スキーマ — 自己記述的であること(bp-python §11)."""

from pydantic import BaseModel

from moka_eval.config import Sampling


class GenerationRecord(BaseModel):
    """1行 = 1試行。後から何の実験だったか分かることが正義."""

    run_id: str
    model_key: str
    model_hf: str
    llamacpp_build: str
    task: str
    article_id: str
    seed: int
    sampling: Sampling
    prompt_sha256: str
    server_flags: list[str]
    ttft_ms: float
    total_ms: float
    prompt_tokens: int
    predicted_tokens: int
    cot_tokens: int
    answer_tokens: int
    pp_tps_derived: float | None
    tg_tps_derived: float | None
    effective_answer_tps: float
    timings: dict[str, float] | None
    cot: str | None
    answer: str
    finish_reason: str
    parse_ok: bool | None = None
    created_at: str


class BlindPair(BaseModel):
    """ブラインド化済みペア。A/B→モデルの対応は別ファイル(key)に隔離."""

    pair_id: str
    article_id: str
    task: str
    text_a: str
    text_b: str


class VerdictRecord(BaseModel):
    """審判1回分(1ペア×1審判×1レンズ、位置スワップ両順込み)."""

    pair_id: str
    judge_key: str
    lens: str
    winner_orig: str
    winner_swap: str
    verdict: str  # 両順一致のみ A/B、他は tie
    analysis_orig: str
    analysis_swap: str
    prompt_sha256: str
    created_at: str


class BenchRecord(BaseModel):
    """速度実測1回分(pp/tg分離、tenets §9 形式)."""

    model_key: str
    model_hf: str
    llamacpp_build: str
    np: int
    rep: int
    pp_n: int
    pp_ms: float
    pp_tps: float
    tg_n: int
    tg_ms: float
    tg_tps: float
    vram_used_gib: float | None
    gtt_used_gib: float | None
    created_at: str
