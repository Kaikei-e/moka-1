"""モデル登録簿 — ADR00006 の repo:quant 文字列とサンプリング設定を唯一の真実とする."""

from pathlib import Path
from typing import Literal

from pydantic import BaseModel

LLAMACPP_BUILD = "b9859"

REPO_ROOT = Path(__file__).resolve().parents[3]
EVAL_ROOT = REPO_ROOT / "eval"
DATA_DIR = EVAL_ROOT / "data"
ARTICLES_DIR = DATA_DIR / "articles"
RETRIEVAL_DIR = DATA_DIR / "retrieval"
PROMPTS_DIR = EVAL_ROOT / "prompts"
RESULTS_DIR = EVAL_ROOT / "results"
SERVER_LOG_DIR = RESULTS_DIR / "server-logs"

CHAT_PORT = 8082
EMBED_PORT = 8083


class Sampling(BaseModel):
    """モデル別ベンダー推奨サンプリング(全レコードに記録する)."""

    temperature: float
    top_p: float | None = None
    top_k: int | None = None
    repeat_penalty: float | None = None

    def as_request_params(self) -> dict[str, float | int]:
        params: dict[str, float | int] = {"temperature": self.temperature}
        if self.top_p is not None:
            params["top_p"] = self.top_p
        if self.top_k is not None:
            params["top_k"] = self.top_k
        if self.repeat_penalty is not None:
            params["repeat_penalty"] = self.repeat_penalty
        return params


class ModelSpec(BaseModel):
    """1候補モデル = 1エントリ。hf は ADR00006 の配布元縛りに従う文字列."""

    key: str
    hf: str
    sampling: Sampling
    thinking: bool = False
    extra_flags: tuple[str, ...] = ()
    mode: Literal["chat", "embed"] = "chat"
    pooling: str | None = None
    query_prefix: str | None = None
    ctx: int | None = None
    # chat template への追加kwargs(例: Qwen系の {"enable_thinking": False})
    chat_kwargs: dict[str, bool | str] | None = None


GREEDY = Sampling(temperature=0.0)
GREEDY_EMBED = Sampling(temperature=0.0)  # embed ではサンプリング未使用(型の都合)

MODELS: dict[str, ModelSpec] = {
    spec.key: spec
    for spec in (
        # 高速パス候補(D1)
        ModelSpec(
            key="lfm25",
            hf="LiquidAI/LFM2.5-8B-A1B-GGUF:Q4_K_M",
            sampling=Sampling(temperature=0.2, top_k=80, repeat_penalty=1.05),
            thinking=True,
        ),
        ModelSpec(
            key="qwen35-4b",
            hf="unsloth/Qwen3.5-4B-GGUF:Q4_K_M",
            # 実測でthinkingデフォルトON(draft_0001の記載と逆)だったため明示的にOFF。
            # 高速パスの実運用形態=非thinking。サンプリングは非thinking推奨値
            sampling=Sampling(temperature=0.7, top_p=0.8, top_k=20),
            thinking=False,
            chat_kwargs={"enable_thinking": False},
        ),
        # 提案層(pin済み、-np 3 劣化率再検証用)
        ModelSpec(
            key="gemma4-e4b",
            hf="unsloth/gemma-4-E4B-it-qat-GGUF:UD-Q4_K_XL",
            sampling=Sampling(temperature=1.0, top_p=0.95, top_k=64),
            thinking=False,
        ),
        # 集約層(D2)+ 審判主審
        ModelSpec(
            key="gptoss20b",
            hf="ggml-org/gpt-oss-20b-GGUF",
            sampling=Sampling(temperature=1.0, top_p=1.0),
            thinking=True,
        ),
        # スワップ枠(D3)
        ModelSpec(
            key="qwen35-35b",
            hf="unsloth/Qwen3.5-35B-A3B-GGUF:UD-Q4_K_XL",
            sampling=Sampling(temperature=0.6, top_p=0.95, top_k=20),
            thinking=True,
        ),
        ModelSpec(
            key="qwen36-35b",
            hf="unsloth/Qwen3.6-35B-A3B-GGUF:UD-Q4_K_XL",
            # ADR00004:29 thinking-mode 推奨値
            sampling=Sampling(temperature=1.0, top_p=0.95, top_k=20),
            thinking=True,
        ),
        # スワップ枠第3候補(Phase R 新モデル掃引): Google公式QAT GGUF、13.9GiB
        ModelSpec(
            key="gemma4-26b",
            hf="google/gemma-4-26B-A4B-it-qat-q4_0-gguf",
            sampling=Sampling(temperature=1.0, top_p=0.95, top_k=64),
            thinking=True,  # 実測でCoTを出す(リキャップで中央値~2500tok)
            extra_flags=("--no-mmproj",),
        ),
        # Embedding候補(D4、Phase R 調査集約で確定)
        ModelSpec(
            key="qwen3-emb-0.6b",
            hf="Qwen/Qwen3-Embedding-0.6B-GGUF:Q8_0",
            sampling=GREEDY_EMBED,
            mode="embed",
            pooling="last",
            query_prefix=(
                "Instruct: Given a web search query, retrieve relevant passages "
                "that answer the query\nQuery: "
            ),
            ctx=8192,
        ),
        ModelSpec(
            key="qwen3-emb-4b",
            hf="Qwen/Qwen3-Embedding-4B-GGUF:Q8_0",
            sampling=GREEDY_EMBED,
            mode="embed",
            pooling="last",
            query_prefix=(
                "Instruct: Given a web search query, retrieve relevant passages "
                "that answer the query\nQuery: "
            ),
            ctx=8192,
        ),
        ModelSpec(
            key="bge-m3",
            hf="ggml-org/bge-m3-Q8_0-GGUF",
            sampling=GREEDY_EMBED,
            mode="embed",
            # pooling は GGUF メタデータ(CLS)に焼き込み済みのため未指定。
            # BERT系(非因果)は入力全体が1 ubatchに収まる必要がある
            extra_flags=("-ub", "8192", "-b", "8192"),
            ctx=8192,
        ),
    )
}


def model(key: str) -> ModelSpec:
    try:
        return MODELS[key]
    except KeyError as err:
        known = ", ".join(sorted(MODELS))
        msg = f"unknown model key {key!r} (known: {known})"
        raise KeyError(msg) from err
