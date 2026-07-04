package feed

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDueFeedLister は DueFeedLister ポートのスクリプト化フェイク。
type fakeDueFeedLister struct {
	feeds []Feed
	err   error
}

func (f *fakeDueFeedLister) DueFeeds(_ context.Context) ([]Feed, error) {
	return f.feeds, f.err
}

// fakeRegisterer は registerer ポートのインメモリフェイク。呼び出し順を記録する。
type fakeRegisterer struct {
	calledURLs []string
	errFor     map[string]error
}

func (f *fakeRegisterer) Register(_ context.Context, rawURL string) (RegisterResult, error) {
	f.calledURLs = append(f.calledURLs, rawURL)
	if err := f.errFor[rawURL]; err != nil {
		return RegisterResult{}, err
	}
	return RegisterResult{}, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestSchedulerTickOnce(t *testing.T) {
	t.Parallel()

	t.Run("registers every due feed in order", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			due := &fakeDueFeedLister{feeds: []Feed{
				{ID: 1, URL: "https://a.example.com/feed"},
				{ID: 2, URL: "https://b.example.com/feed"},
				{ID: 3, URL: "https://c.example.com/feed"},
			}}
			reg := &fakeRegisterer{}
			s := NewScheduler(due, reg, time.Hour, discardLogger())

			s.tickOnce(t.Context())

			assert.Equal(t, []string{
				"https://a.example.com/feed",
				"https://b.example.com/feed",
				"https://c.example.com/feed",
			}, reg.calledURLs)
		})
	})

	t.Run("continues to the next feed when one Register call fails (fail-soft)", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			due := &fakeDueFeedLister{feeds: []Feed{
				{ID: 1, URL: "https://a.example.com/feed"},
				{ID: 2, URL: "https://b.example.com/feed"},
			}}
			reg := &fakeRegisterer{errFor: map[string]error{
				"https://a.example.com/feed": errors.New("boom"),
			}}
			s := NewScheduler(due, reg, time.Hour, discardLogger())

			s.tickOnce(t.Context())

			assert.Equal(t, []string{
				"https://a.example.com/feed",
				"https://b.example.com/feed",
			}, reg.calledURLs, "1件目が失敗しても2件目は呼ばれる")
		})
	})

	t.Run("stops iterating once the context is cancelled", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			due := &fakeDueFeedLister{feeds: []Feed{
				{ID: 1, URL: "https://a.example.com/feed"},
				{ID: 2, URL: "https://b.example.com/feed"},
			}}
			reg := &fakeRegisterer{}
			s := NewScheduler(due, reg, time.Hour, discardLogger())

			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			s.tickOnce(ctx)

			assert.Empty(t, reg.calledURLs, "cancel 済みの ctx では1件も呼ばない")
		})
	})

	t.Run("DueFeeds failure is reported without a fetch attempt", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			due := &fakeDueFeedLister{err: errors.New("db down")}
			reg := &fakeRegisterer{}
			s := NewScheduler(due, reg, time.Hour, discardLogger())

			s.tickOnce(t.Context())

			assert.Empty(t, reg.calledURLs)
		})
	})
}

func TestNewSchedulerDefaultsNonPositiveTick(t *testing.T) {
	t.Parallel()

	s := NewScheduler(&fakeDueFeedLister{}, &fakeRegisterer{}, 0, discardLogger())
	assert.Equal(t, defaultTick, s.tick)

	s = NewScheduler(&fakeDueFeedLister{}, &fakeRegisterer{}, -time.Second, discardLogger())
	assert.Equal(t, defaultTick, s.tick)
}

func TestSchedulerRun(t *testing.T) {
	t.Parallel()

	t.Run("ticks at the configured interval until the context is cancelled", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			due := &fakeDueFeedLister{feeds: []Feed{{ID: 1, URL: "https://a.example.com/feed"}}}
			reg := &fakeRegisterer{}
			s := NewScheduler(due, reg, 10*time.Second, discardLogger())

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() {
				s.Run(ctx)
				close(done)
			}()

			synctest.Wait()
			require.Len(t, reg.calledURLs, 1, "起動直後に1回 due 判定する")

			time.Sleep(10 * time.Second)
			synctest.Wait()
			require.Len(t, reg.calledURLs, 2, "tick 間隔ごとに再度 due 判定する")

			time.Sleep(10 * time.Second)
			synctest.Wait()
			require.Len(t, reg.calledURLs, 3)

			cancel()
			synctest.Wait()
			select {
			case <-done:
			default:
				t.Fatal("Run はループ内で ctx cancel を検知して即座に返るはず")
			}
		})
	})
}
