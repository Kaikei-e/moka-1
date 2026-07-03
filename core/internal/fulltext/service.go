package fulltext

import (
	"context"
	"fmt"
)

// Result は取り寄せユースケースの結果。Created は HTTP 層が 201/200 の判定に使う。
type Result struct {
	FullText FullText
	Created  bool
}

// Service は取り寄せユースケース: 保存済みならそれを返す(冪等) → 無ければ
// 検証 → 取得 → 抽出 → 保存。interface にのみ依存し、具象は main が注入する。
type Service struct {
	store    Store
	fetch    PageFetcher
	extract  Extractor
	validate Validator
}

// NewService はポートの具象を受け取って取り寄せユースケースを組む(呼び出しは main のみ)。
func NewService(store Store, fetch PageFetcher, extract Extractor, validate Validator) *Service {
	return &Service{store: store, fetch: fetch, extract: extract, validate: validate}
}

// FetchFullText は articleID の全文を取り寄せる。既に保存済みなら外部サイトへは
// 取りに行かず、その行をそのまま返す(冪等 — ADR00010 の登録と同じ思想)。
func (s *Service) FetchFullText(ctx context.Context, articleID int64, articleURL string) (Result, error) {
	existing, found, err := s.store.LatestFullText(ctx, articleID)
	if err != nil {
		return Result{}, fmt.Errorf("lookup fulltext %d: %w", articleID, err)
	}
	if found {
		return Result{FullText: existing, Created: false}, nil
	}

	if err := s.validate.Validate(ctx, articleURL); err != nil {
		return Result{}, fmt.Errorf("validate %s: %w", articleURL, err)
	}

	html, err := s.fetch.FetchPage(ctx, articleURL)
	if err != nil {
		return Result{}, fmt.Errorf("fetch %s: %w", articleURL, err)
	}

	text, err := s.extract.Extract(html, articleURL)
	if err != nil {
		return Result{}, fmt.Errorf("extract %s: %w", articleURL, err)
	}

	ft, err := s.store.InsertFullText(ctx, articleID, text)
	if err != nil {
		return Result{}, fmt.Errorf("insert fulltext %d: %w", articleID, err)
	}
	return Result{FullText: ft, Created: true}, nil
}
