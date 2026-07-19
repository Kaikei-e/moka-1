package rag

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// rrfK は Reciprocal Rank Fusion の定数 k(ADR00022 の既定値 60)。重みの実測調整は
// eval/ の retrieval ハーネスの仕事で、ここでは標準形 1/(k+rank) を使う。
const rrfK = 60

// TextSearcher は pg_trgm によるテキスト検索の消費側ポート(具象は *store.Store)。
// 戻り値は類似度の高い順(順位だけを RRF に使う)。
type TextSearcher interface {
	SearchArticlesByText(ctx context.Context, q string, limit int) ([]feed.Article, error)
}

// VectorSearcher は pgvector cosine 近傍検索の消費側ポート(具象は *store.Store)。
// 記事ごと最新の埋め込みに対する近い順。
type VectorSearcher interface {
	SearchArticlesByVector(ctx context.Context, vector []float32, limit int) ([]feed.Article, error)
}

// QueryEmbedder はクエリ埋め込みの消費側ポート(具象は *LLMEmbedder)。
type QueryEmbedder interface {
	EmbedQuery(ctx context.Context, q string) ([]float32, error)
}

// Searcher はハイブリッド検索ユースケース: pg_trgm 類似度と pgvector cosine 近傍を
// RRF(k=60)で融合する(ADR00022)。llm が死んでいてクエリ埋め込みが取れない場合は
// テキスト側単独に縮退する(フェイルソフト — 検索は 200 を返し続ける)。
type Searcher struct {
	text  TextSearcher
	vec   VectorSearcher
	embed QueryEmbedder
	log   *slog.Logger
}

// NewSearcher はポートの具象を受け取ってハイブリッド検索を組む(呼び出しは main のみ)。
func NewSearcher(text TextSearcher, vec VectorSearcher, embed QueryEmbedder, log *slog.Logger) *Searcher {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Searcher{text: text, vec: vec, embed: embed, log: log}
}

// Search は q に対する上位 limit 件を返す(httpapi.ArticleSearcher / ContextSearcher)。
func (s *Searcher) Search(ctx context.Context, q string, limit int) ([]SearchHit, error) {
	textHits, err := s.text.SearchArticlesByText(ctx, q, limit)
	if err != nil {
		// テキスト側は DB そのものなので縮退先が無い(DB 停止はフェイルソフトの対象外)
		return nil, fmt.Errorf("text search: %w", err)
	}

	vecHits := s.vectorHits(ctx, q, limit)
	return fuseRRF(textHits, vecHits, limit), nil
}

// vectorHits はクエリ埋め込み → 近傍検索のベクトル側を引く。llm 停止(埋め込み不可)や
// ベクトル側の失敗は warn ログだけ残して nil を返し、テキスト側単独に縮退させる。
func (s *Searcher) vectorHits(ctx context.Context, q string, limit int) []feed.Article {
	vec, err := s.embed.EmbedQuery(ctx, q)
	if err != nil {
		s.log.Warn("embed query failed, degrading to text-only search", "err", err.Error())
		return nil
	}

	hits, err := s.vec.SearchArticlesByVector(ctx, vec, limit)
	if err != nil {
		s.log.Warn("vector search failed, degrading to text-only search", "err", err.Error())
		return nil
	}
	return hits
}

// fuseRRF は2つの順位付きリストを Reciprocal Rank Fusion(score = Σ 1/(k+rank)、rank は
// 1始まり)で融合し、スコア降順(同点は id 降順 = 新しい記事優先)の上位 limit 件を返す。
func fuseRRF(textHits, vecHits []feed.Article, limit int) []SearchHit {
	byID := make(map[int64]*SearchHit)
	order := make([]int64, 0, len(textHits)+len(vecHits))

	accumulate := func(hits []feed.Article) {
		for rank, a := range hits {
			h, ok := byID[a.ID]
			if !ok {
				h = &SearchHit{Article: a}
				byID[a.ID] = h
				order = append(order, a.ID)
			}
			h.Score += 1.0 / float64(rrfK+rank+1)
		}
	}
	accumulate(textHits)
	accumulate(vecHits)

	fused := make([]SearchHit, 0, len(order))
	for _, id := range order {
		fused = append(fused, *byID[id])
	}
	slices.SortStableFunc(fused, func(a, b SearchHit) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		case a.ID > b.ID:
			return -1
		case a.ID < b.ID:
			return 1
		default:
			return 0
		}
	})

	if len(fused) > limit {
		fused = fused[:limit]
	}
	return fused
}
