package feed

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStore は Store ポートのインメモリフェイク。呼び出しを記録する。
type fakeStore struct {
	existing      *Feed       // FeedByURL が返す既存フィード(nil = 未登録)
	cond          Conditional // LatestFetchConditional が返す状態
	nextID        int64
	insertedFeeds []struct{ URL, Title string }
	fetchRecords  []FetchRecord
	insertedItems []Item
	insertReturn  int
	insertErr     error // InsertArticles を失敗させる(部分失敗の再現用)
}

func (s *fakeStore) FeedByURL(_ context.Context, url string) (Feed, bool, error) {
	if s.existing != nil && s.existing.URL == url {
		return *s.existing, true, nil
	}
	return Feed{}, false, nil
}

func (s *fakeStore) InsertFeed(_ context.Context, url, title string) (Feed, error) {
	s.insertedFeeds = append(s.insertedFeeds, struct{ URL, Title string }{url, title})
	s.nextID++
	return Feed{ID: s.nextID, URL: url, Title: title, CreatedAt: time.Now()}, nil
}

func (s *fakeStore) LatestFetchConditional(_ context.Context, _ int64) (Conditional, error) {
	return s.cond, nil
}

func (s *fakeStore) InsertFeedFetch(_ context.Context, _ int64, rec FetchRecord) error {
	s.fetchRecords = append(s.fetchRecords, rec)
	return nil
}

func (s *fakeStore) InsertArticles(_ context.Context, _ int64, items []Item) (int, error) {
	if s.insertErr != nil {
		return 0, s.insertErr
	}
	s.insertedItems = append(s.insertedItems, items...)
	return s.insertReturn, nil
}

// fakeFetcher は Fetcher ポートのスクリプト化フェイク。
type fakeFetcher struct {
	result   FetchResult
	err      error
	gotURL   string
	gotCond  Conditional
	numCalls int
}

func (f *fakeFetcher) Fetch(_ context.Context, url string, cond Conditional) (FetchResult, error) {
	f.numCalls++
	f.gotURL = url
	f.gotCond = cond
	return f.result, f.err
}

func newTestRegistrar(s *fakeStore, f *fakeFetcher) *Registrar {
	return NewRegistrar(s, f, NewURLValidator(true), slog.New(slog.DiscardHandler))
}

func TestRegistrarRegister(t *testing.T) {
	t.Parallel()

	items := []Item{
		{GUID: "g1", URL: "http://example.com/1", Title: "One"},
		{GUID: "g2", URL: "http://example.com/2", Title: "Two"},
	}
	okFetch := FetchResult{StatusCode: 200, ETag: `"v1"`, LastModified: "lm1", Title: "Example", Items: items}

	t.Run("new feed is fetched, inserted and articles stored", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{insertReturn: 2}
		fetcher := &fakeFetcher{result: okFetch}
		res, err := newTestRegistrar(store, fetcher).Register(t.Context(), "http://example.com/feed.xml")
		require.NoError(t, err)

		assert.True(t, res.Created)
		assert.Equal(t, 2, res.InsertedArticles)
		assert.Equal(t, "Example", res.Feed.Title, "feeds.title はパース結果から INSERT 時に一度だけ")
		assert.Equal(t, Conditional{}, fetcher.gotCond, "新規フィードは条件無しで取得")

		require.Len(t, store.insertedFeeds, 1)
		assert.Equal(t, "Example", store.insertedFeeds[0].Title)
		require.Len(t, store.fetchRecords, 1)
		assert.Equal(t, FetchRecord{StatusCode: 200, ETag: `"v1"`, LastModified: "lm1"}, store.fetchRecords[0])
		assert.Len(t, store.insertedItems, 2)
	})

	t.Run("existing feed passes conditional state and 304 skips articles", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{
			existing: &Feed{ID: 7, URL: "http://example.com/feed.xml", Title: "Example"},
			cond:     Conditional{ETag: `"v1"`, LastModified: "lm1"},
		}
		fetcher := &fakeFetcher{result: FetchResult{StatusCode: 304, NotModified: true, ETag: `"v1"`}}
		res, err := newTestRegistrar(store, fetcher).Register(t.Context(), "http://example.com/feed.xml")
		require.NoError(t, err)

		assert.False(t, res.Created)
		assert.Equal(t, 0, res.InsertedArticles)
		assert.Equal(t, int64(7), res.Feed.ID)
		assert.Equal(t, Conditional{ETag: `"v1"`, LastModified: "lm1"}, fetcher.gotCond)

		assert.Empty(t, store.insertedFeeds, "既存フィードを再 INSERT しない")
		assert.Empty(t, store.insertedItems, "304 なら記事を触らない")
		require.Len(t, store.fetchRecords, 1, "304 もイベントとして追記する")
		assert.Equal(t, 304, store.fetchRecords[0].StatusCode)
	})

	t.Run("existing feed with new items inserts them", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{
			existing:     &Feed{ID: 7, URL: "http://example.com/feed.xml", Title: "Example"},
			insertReturn: 1,
		}
		fetcher := &fakeFetcher{result: okFetch}
		res, err := newTestRegistrar(store, fetcher).Register(t.Context(), "http://example.com/feed.xml")
		require.NoError(t, err)

		assert.False(t, res.Created)
		assert.Equal(t, 1, res.InsertedArticles, "挿入数は store の ON CONFLICT 結果")
		assert.Len(t, store.insertedItems, 2)
	})

	t.Run("fetch failure on existing feed records error event and returns error", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{existing: &Feed{ID: 7, URL: "http://example.com/feed.xml"}}
		fetcher := &fakeFetcher{
			result: FetchResult{StatusCode: 500},
			err:    fmt.Errorf("status 500: %w", ErrUpstreamFetch),
		}
		_, err := newTestRegistrar(store, fetcher).Register(t.Context(), "http://example.com/feed.xml")
		require.ErrorIs(t, err, ErrUpstreamFetch)

		require.Len(t, store.fetchRecords, 1, "失敗もイベントとして追記する")
		assert.Equal(t, 500, store.fetchRecords[0].StatusCode)
		assert.NotEmpty(t, store.fetchRecords[0].Error)
		assert.Empty(t, store.insertedItems)
	})

	t.Run("fetch failure on new feed persists nothing", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		fetcher := &fakeFetcher{err: fmt.Errorf("boom: %w", ErrUpstreamFetch)}
		_, err := newTestRegistrar(store, fetcher).Register(t.Context(), "http://example.com/feed.xml")
		require.ErrorIs(t, err, ErrUpstreamFetch)

		assert.Empty(t, store.insertedFeeds, "孤児 feed 行を作らない")
		assert.Empty(t, store.fetchRecords, "feed が無いのでイベントも無い")
		assert.Empty(t, store.insertedItems)
	})

	t.Run("invalid url on unknown feed persists nothing and never fetches", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		fetcher := &fakeFetcher{}
		reg := NewRegistrar(store, fetcher, NewURLValidator(false), slog.New(slog.DiscardHandler))

		_, err := reg.Register(t.Context(), "http://127.0.0.1/feed")
		require.ErrorIs(t, err, ErrPrivateHost)
		assert.Zero(t, fetcher.numCalls)
		assert.Empty(t, store.insertedFeeds)
		assert.Empty(t, store.fetchRecords)
	})

	t.Run("invalid url on existing feed records an error event (backoff instead of every-tick retry)", func(t *testing.T) {
		t.Parallel()

		// 登録後にドメインが解決不能/プライベート化したフィード: イベントを記録しないと
		// due 判定が下がらず、スケジューラが毎 tick 再試行し続ける
		store := &fakeStore{existing: &Feed{ID: 7, URL: "http://127.0.0.1/feed"}}
		fetcher := &fakeFetcher{}
		reg := NewRegistrar(store, fetcher, NewURLValidator(false), slog.New(slog.DiscardHandler))

		_, err := reg.Register(t.Context(), "http://127.0.0.1/feed")
		require.ErrorIs(t, err, ErrPrivateHost)
		assert.Zero(t, fetcher.numCalls)

		require.Len(t, store.fetchRecords, 1, "検証失敗もイベントとして追記する")
		assert.NotEmpty(t, store.fetchRecords[0].Error)
		assert.Empty(t, store.fetchRecords[0].ETag, "検証失敗イベントは検証子を持たない")
	})

	t.Run("article insert failure does not commit the new conditional-GET state", func(t *testing.T) {
		t.Parallel()

		// 検証子(ETag)を記事より先にコミットすると、記事挿入が失敗した瞬間に
		// 以後 304 が返り続け、その回の記事を恒久に失う
		store := &fakeStore{
			existing:  &Feed{ID: 7, URL: "http://example.com/feed.xml"},
			insertErr: assert.AnError,
		}
		fetcher := &fakeFetcher{result: okFetch}
		_, err := newTestRegistrar(store, fetcher).Register(t.Context(), "http://example.com/feed.xml")
		require.ErrorIs(t, err, assert.AnError)

		require.Len(t, store.fetchRecords, 1, "失敗イベントは残す(バックオフ用)")
		rec := store.fetchRecords[0]
		assert.NotEmpty(t, rec.Error)
		assert.Empty(t, rec.ETag, "新しい ETag を確定させない(次回取得で再処理させる)")
		assert.Empty(t, rec.LastModified)
	})

	t.Run("success records the conditional-GET state only after articles are stored", func(t *testing.T) {
		t.Parallel()

		calls := &callOrderStore{fakeStore: fakeStore{
			existing:     &Feed{ID: 7, URL: "http://example.com/feed.xml"},
			insertReturn: 2,
		}}
		fetcher := &fakeFetcher{result: okFetch}
		reg := NewRegistrar(calls, fetcher, NewURLValidator(true), slog.New(slog.DiscardHandler))
		_, err := reg.Register(t.Context(), "http://example.com/feed.xml")
		require.NoError(t, err)

		require.Equal(t, []string{"InsertArticles", "InsertFeedFetch"}, calls.order,
			"記事挿入 → 検証子コミット の順(逆だと部分失敗で記事を失う)")
	})
}

// callOrderStore は書き込み系呼び出しの順序を記録する(C-1 の順序保証テスト用)。
type callOrderStore struct {
	fakeStore
	order []string
}

func (s *callOrderStore) InsertArticles(ctx context.Context, feedID int64, items []Item) (int, error) {
	s.order = append(s.order, "InsertArticles")
	return s.fakeStore.InsertArticles(ctx, feedID, items)
}

func (s *callOrderStore) InsertFeedFetch(ctx context.Context, feedID int64, rec FetchRecord) error {
	s.order = append(s.order, "InsertFeedFetch")
	return s.fakeStore.InsertFeedFetch(ctx, feedID, rec)
}
