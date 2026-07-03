package fulltext

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kaikei-e/moka-1/core/internal/feed"
)

// fakeStore は Store ポートのインメモリフェイク。
type fakeStore struct {
	existing *FullText
	nextID   int64
	inserted []struct {
		ArticleID int64
		Text      string
	}
}

func (s *fakeStore) LatestFullText(_ context.Context, articleID int64) (FullText, bool, error) {
	if s.existing != nil && s.existing.ArticleID == articleID {
		return *s.existing, true, nil
	}
	return FullText{}, false, nil
}

func (s *fakeStore) InsertFullText(_ context.Context, articleID int64, text string) (FullText, error) {
	s.nextID++
	s.inserted = append(s.inserted, struct {
		ArticleID int64
		Text      string
	}{articleID, text})
	return FullText{ArticleID: articleID, Text: text, FetchedAt: time.Now()}, nil
}

// fakeFetcher は PageFetcher ポートのスクリプト化フェイク。
type fakeFetcher struct {
	html     []byte
	err      error
	gotURL   string
	numCalls int
}

func (f *fakeFetcher) FetchPage(_ context.Context, url string) ([]byte, error) {
	f.numCalls++
	f.gotURL = url
	return f.html, f.err
}

// fakeExtractor は Extractor ポートのスクリプト化フェイク。
type fakeExtractor struct {
	text     string
	err      error
	gotHTML  []byte
	numCalls int
}

func (e *fakeExtractor) Extract(html []byte, _ string) (string, error) {
	e.numCalls++
	e.gotHTML = html
	return e.text, e.err
}

// fakeValidator は Validator ポートのスクリプト化フェイク。
type fakeValidator struct {
	err      error
	numCalls int
}

func (v *fakeValidator) Validate(_ context.Context, _ string) error {
	v.numCalls++
	return v.err
}

func TestServiceFetchFullText(t *testing.T) {
	t.Parallel()

	t.Run("already stored returns existing row without touching the network", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{existing: &FullText{ArticleID: 7, Text: "既存の全文"}}
		fetcher := &fakeFetcher{}
		extractor := &fakeExtractor{}
		validator := &fakeValidator{}
		svc := NewService(store, fetcher, extractor, validator)

		res, err := svc.FetchFullText(t.Context(), 7, "http://example.com/a")
		require.NoError(t, err)

		assert.False(t, res.Created, "冪等 — 既存行は再取得しない")
		assert.Equal(t, "既存の全文", res.FullText.Text)
		assert.Zero(t, validator.numCalls, "既存行があれば URL 検証もしない")
		assert.Zero(t, fetcher.numCalls)
		assert.Zero(t, extractor.numCalls)
		assert.Empty(t, store.inserted)
	})

	t.Run("not stored yet validates, fetches, extracts and stores", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		fetcher := &fakeFetcher{html: []byte("<html><body>本文</body></html>")}
		extractor := &fakeExtractor{text: "抽出された本文"}
		validator := &fakeValidator{}
		svc := NewService(store, fetcher, extractor, validator)

		res, err := svc.FetchFullText(t.Context(), 7, "http://example.com/a")
		require.NoError(t, err)

		assert.True(t, res.Created)
		assert.Equal(t, "抽出された本文", res.FullText.Text)
		assert.Equal(t, int64(7), res.FullText.ArticleID)
		assert.Equal(t, 1, validator.numCalls)
		assert.Equal(t, "http://example.com/a", fetcher.gotURL)
		assert.Equal(t, []byte("<html><body>本文</body></html>"), extractor.gotHTML)

		require.Len(t, store.inserted, 1)
		assert.Equal(t, int64(7), store.inserted[0].ArticleID)
		assert.Equal(t, "抽出された本文", store.inserted[0].Text)
	})

	t.Run("invalid url short-circuits before fetch and extract", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		fetcher := &fakeFetcher{}
		extractor := &fakeExtractor{}
		validator := &fakeValidator{err: fmt.Errorf("private: %w", feed.ErrPrivateHost)}
		svc := NewService(store, fetcher, extractor, validator)

		_, err := svc.FetchFullText(t.Context(), 7, "http://127.0.0.1/a")
		require.ErrorIs(t, err, ErrPrivateHost)
		assert.Zero(t, fetcher.numCalls)
		assert.Zero(t, extractor.numCalls)
		assert.Empty(t, store.inserted)
	})

	t.Run("fetch failure stores nothing", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		fetcher := &fakeFetcher{err: fmt.Errorf("boom: %w", ErrUpstreamFetch)}
		extractor := &fakeExtractor{}
		validator := &fakeValidator{}
		svc := NewService(store, fetcher, extractor, validator)

		_, err := svc.FetchFullText(t.Context(), 7, "http://example.com/a")
		require.ErrorIs(t, err, ErrUpstreamFetch)
		assert.Zero(t, extractor.numCalls)
		assert.Empty(t, store.inserted)
	})

	t.Run("extract failure stores nothing", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{}
		fetcher := &fakeFetcher{html: []byte("<html></html>")}
		extractor := &fakeExtractor{err: fmt.Errorf("empty: %w", ErrExtractFailed)}
		validator := &fakeValidator{}
		svc := NewService(store, fetcher, extractor, validator)

		_, err := svc.FetchFullText(t.Context(), 7, "http://example.com/a")
		require.ErrorIs(t, err, ErrExtractFailed)
		assert.Empty(t, store.inserted)
	})

	t.Run("store lookup failure short-circuits before validation", func(t *testing.T) {
		t.Parallel()

		store := &fakeStoreErr{err: assert.AnError}
		fetcher := &fakeFetcher{}
		extractor := &fakeExtractor{}
		validator := &fakeValidator{}
		svc := NewService(store, fetcher, extractor, validator)

		_, err := svc.FetchFullText(t.Context(), 7, "http://example.com/a")
		require.Error(t, err)
		assert.Zero(t, validator.numCalls)
		assert.Zero(t, fetcher.numCalls)
	})
}

// fakeStoreErr は LatestFullText が常に失敗する Store フェイク。
type fakeStoreErr struct {
	err error
}

func (s *fakeStoreErr) LatestFullText(_ context.Context, _ int64) (FullText, bool, error) {
	return FullText{}, false, s.err
}

func (s *fakeStoreErr) InsertFullText(_ context.Context, articleID int64, text string) (FullText, error) {
	return FullText{ArticleID: articleID, Text: text}, nil
}
