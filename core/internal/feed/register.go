package feed

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Store は登録ユースケースの永続化ポート(消費側定義 — 具象は internal/store)。
type Store interface {
	FeedByURL(ctx context.Context, url string) (Feed, bool, error)
	InsertFeed(ctx context.Context, url, title string) (Feed, error)
	LatestFetchConditional(ctx context.Context, feedID int64) (Conditional, error)
	InsertFeedFetch(ctx context.Context, feedID int64, rec FetchRecord) error
	InsertArticles(ctx context.Context, feedID int64, items []Item) (int, error)
}

// Fetcher は外部フィード HTTP のポート(具象は HTTPFetcher)。
type Fetcher interface {
	Fetch(ctx context.Context, url string, cond Conditional) (FetchResult, error)
}

// Registrar はフィード登録ユースケース: 検証 → 取得 → 保存(冪等)。
// interface にのみ依存し、具象は cmd/moka/main.go が注入する。
// 注: 外部取得のグローバルレートリミッタ(≥5s 間隔、tenets §3.2 / §8 未決事項5)は
// Scheduler 側が持つ(Scheduler.tickOnce)。ユーザー起点の同期登録(POST /api/v1/feeds)は
// 1 リクエストなのでバーストの懸念が無く、Registrar 自体は無制限のまま呼ぶ。
type Registrar struct {
	store    Store
	fetch    Fetcher
	validate *URLValidator
	log      *slog.Logger
}

// fetchTimeout は 1 回のフィード取得のデッドライン(bp-go §6)。
const fetchTimeout = 30 * time.Second

// NewRegistrar はポートの具象を受け取って登録ユースケースを組む(呼び出しは main のみ)。
func NewRegistrar(store Store, fetch Fetcher, v *URLValidator, log *slog.Logger) *Registrar {
	return &Registrar{store: store, fetch: fetch, validate: v, log: log}
}

// Register は URL を検証し、フィードを取得して保存する。
// 既存フィードなら条件付き GET で再取得(304 は記事を触らない)。冪等。
// 既存フィードの取得失敗は feed_fetches に error イベントを追記してからエラーを返す。
// 新規フィードの取得失敗は何も永続化しない(孤児 feed 行を作らない)。
func (r *Registrar) Register(ctx context.Context, rawURL string) (RegisterResult, error) {
	if err := r.validate.Validate(ctx, rawURL); err != nil {
		return RegisterResult{}, fmt.Errorf("validate %s: %w", rawURL, err)
	}

	existing, found, err := r.store.FeedByURL(ctx, rawURL)
	if err != nil {
		return RegisterResult{}, fmt.Errorf("lookup feed %s: %w", rawURL, err)
	}

	var cond Conditional
	if found {
		cond, err = r.store.LatestFetchConditional(ctx, existing.ID)
		if err != nil {
			return RegisterResult{}, fmt.Errorf("latest fetch state feed %d: %w", existing.ID, err)
		}
	}

	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	res, fetchErr := r.fetch.Fetch(fetchCtx, rawURL, cond)
	if fetchErr != nil {
		if found {
			rec := FetchRecord{
				StatusCode:   res.StatusCode,
				ETag:         res.ETag,
				LastModified: res.LastModified,
				Error:        fetchErr.Error(),
			}
			// 失敗イベントの記録失敗はログに落として本来のエラーを返す(fail-soft)
			if recErr := r.store.InsertFeedFetch(ctx, existing.ID, rec); recErr != nil {
				r.log.Warn("record fetch failure", "feed_id", existing.ID, "err", recErr.Error())
			}
		}
		return RegisterResult{}, fmt.Errorf("register %s: %w", rawURL, fetchErr)
	}

	f := existing
	created := false
	if !found {
		f, err = r.store.InsertFeed(ctx, rawURL, res.Title)
		if err != nil {
			return RegisterResult{}, fmt.Errorf("insert feed %s: %w", rawURL, err)
		}
		created = true
	}

	rec := FetchRecord{StatusCode: res.StatusCode, ETag: res.ETag, LastModified: res.LastModified}
	if err := r.store.InsertFeedFetch(ctx, f.ID, rec); err != nil {
		return RegisterResult{}, fmt.Errorf("record fetch feed %d: %w", f.ID, err)
	}

	inserted := 0
	if !res.NotModified && len(res.Items) > 0 {
		inserted, err = r.store.InsertArticles(ctx, f.ID, res.Items)
		if err != nil {
			return RegisterResult{}, fmt.Errorf("insert articles feed %d: %w", f.ID, err)
		}
	}

	r.log.Info("feed registered", "feed_id", f.ID, "created", created, "inserted_articles", inserted)
	return RegisterResult{Feed: f, Created: created, InsertedArticles: inserted}, nil
}
