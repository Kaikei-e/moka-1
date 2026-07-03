"""Embedding retrieval 評価 — recall@k / MRR、MRL次元切り詰め再スコア."""

import json
import math
from datetime import UTC, datetime
from typing import Any

from pydantic import BaseModel

from moka_eval.client import LlamaClient
from moka_eval.config import RESULTS_DIR, RETRIEVAL_DIR, ModelSpec
from moka_eval.dataset import load_articles

CORPUS_MAX_CHARS = 6000


class Query(BaseModel):
    query: str
    gold_article_id: str
    style: str  # "keyword" | "natural"


class EmbedEvalError(RuntimeError):
    """retrieval 評価の失敗."""


def cosine(a: list[float], b: list[float]) -> float:
    dot = sum(x * y for x, y in zip(a, b, strict=True))
    norm_a = math.sqrt(sum(x * x for x in a))
    norm_b = math.sqrt(sum(y * y for y in b))
    if norm_a == 0.0 or norm_b == 0.0:
        return 0.0
    return dot / (norm_a * norm_b)


def truncate_normalize(vec: list[float], dim: int) -> list[float]:
    """MRL: 先頭 dim 次元に切り詰めて再正規化."""
    head = vec[:dim]
    norm = math.sqrt(sum(x * x for x in head))
    if norm == 0.0:
        return head
    return [x / norm for x in head]


def rank_of_gold(ranked_ids: list[str], gold: str) -> int | None:
    """gold の 1-based 順位。圏外は None."""
    try:
        return ranked_ids.index(gold) + 1
    except ValueError:
        return None


def recall_at_k(ranks: list[int | None], k: int) -> float:
    if not ranks:
        return 0.0
    hits = sum(1 for r in ranks if r is not None and r <= k)
    return hits / len(ranks)


def mrr(ranks: list[int | None]) -> float:
    if not ranks:
        return 0.0
    return sum(1.0 / r for r in ranks if r is not None) / len(ranks)


def load_queries() -> list[Query]:
    path = RETRIEVAL_DIR / "queries.json"
    if not path.is_file():
        msg = f"queries missing: {path}"
        raise EmbedEvalError(msg)
    return [Query.model_validate(q) for q in json.loads(path.read_text(encoding="utf-8"))]


def _metrics_at_dim(
    corpus: dict[str, list[float]],
    query_vecs: list[tuple[Query, list[float]]],
    dim: int | None,
) -> dict[str, float]:
    if dim is not None:
        corpus = {aid: truncate_normalize(v, dim) for aid, v in corpus.items()}
    ranks: list[int | None] = []
    for query, vec in query_vecs:
        q = truncate_normalize(vec, dim) if dim is not None else vec
        ranked = sorted(corpus, key=lambda aid: cosine(q, corpus[aid]), reverse=True)
        ranks.append(rank_of_gold(ranked, query.gold_article_id))
    return {
        "recall@1": round(recall_at_k(ranks, 1), 4),
        "recall@3": round(recall_at_k(ranks, 3), 4),
        "mrr": round(mrr(ranks), 4),
    }


def run_embed_eval(
    client: LlamaClient, spec: ModelSpec, *, mrl_dims: tuple[int, ...] = ()
) -> dict[str, Any]:
    """コーパス+クエリを埋め込み、フル次元と MRL 切り詰めでスコアする."""
    articles = load_articles()
    queries = load_queries()
    corpus_texts = [f"{a.title}\n{a.text[:CORPUS_MAX_CHARS]}" for a in articles]
    corpus_vecs = client.embed(corpus_texts)
    corpus = {a.id: v for a, v in zip(articles, corpus_vecs, strict=True)}

    prefix = spec.query_prefix or ""
    query_vecs_raw = client.embed([f"{prefix}{q.query}" for q in queries])
    query_vecs = list(zip(queries, query_vecs_raw, strict=True))

    full_dim = len(corpus_vecs[0])
    by_dim: dict[str, dict[str, float]] = {str(full_dim): _metrics_at_dim(corpus, query_vecs, None)}
    for dim in mrl_dims:
        if dim < full_dim:
            by_dim[str(dim)] = _metrics_at_dim(corpus, query_vecs, dim)

    summary: dict[str, Any] = {
        "model_key": spec.key,
        "model_hf": spec.hf,
        "pooling": spec.pooling,
        "query_prefix": spec.query_prefix,
        "full_dim": full_dim,
        "n_corpus": len(corpus),
        "n_queries": len(queries),
        "metrics_by_dim": by_dim,
        "created_at": datetime.now(UTC).isoformat(timespec="seconds"),
    }
    out_dir = RESULTS_DIR / "summary"
    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / f"embed-{spec.key}.json").write_text(
        json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8"
    )
    return summary
