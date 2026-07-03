package feed

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArticleCursorRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("round-trips published_at and id", func(t *testing.T) {
		t.Parallel()

		ts := time.Date(2026, 7, 1, 9, 0, 0, 123456000, time.UTC) // timestamptz はマイクロ秒精度
		c := ArticleCursor{PublishedAt: &ts, ID: 42}

		got, err := DecodeArticleCursor(c.Encode())
		require.NoError(t, err)
		require.NotNil(t, got.PublishedAt)
		assert.True(t, got.PublishedAt.Equal(ts), "published_at はナノ秒まで等値であること")
		assert.Equal(t, int64(42), got.ID)
	})

	t.Run("round-trips a null published_at (NULLS LAST の末尾領域)", func(t *testing.T) {
		t.Parallel()

		c := ArticleCursor{PublishedAt: nil, ID: 7}

		got, err := DecodeArticleCursor(c.Encode())
		require.NoError(t, err)
		assert.Nil(t, got.PublishedAt)
		assert.Equal(t, int64(7), got.ID)
	})
}

func TestDecodeArticleCursorRejectsGarbage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{name: "not base64url", input: "%%%"},
		{name: "no separator", input: "bm8tc2VwYXJhdG9y"},    // "no-separator"
		{name: "bad timestamp", input: "bm90LWEtdGltZXwxMg"}, // "not-a-time|12"
		{name: "bad id", input: "fGFiYw"},                    // "|abc"
		{name: "empty", input: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeArticleCursor(tt.input)
			assert.ErrorIs(t, err, ErrInvalidCursor)
		})
	}
}
