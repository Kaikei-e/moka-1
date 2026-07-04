package feed

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArticleCursorRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("round-trips sort key and id", func(t *testing.T) {
		t.Parallel()

		ts := time.Date(2026, 7, 1, 9, 0, 0, 123456000, time.UTC) // timestamptz はマイクロ秒精度
		c := ArticleCursor{SortKey: ts, ID: 42}

		got, err := DecodeArticleCursor(c.Encode())
		require.NoError(t, err)
		assert.True(t, got.SortKey.Equal(ts), "sort key はナノ秒まで等値であること")
		assert.Equal(t, int64(42), got.ID)
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

	t.Run("old-format cursor with an empty timestamp (pre-SortKey PublishedAt==nil) is now invalid", func(t *testing.T) {
		t.Parallel()

		oldFormat := base64.RawURLEncoding.EncodeToString([]byte("|7"))
		_, err := DecodeArticleCursor(oldFormat)
		assert.ErrorIs(t, err, ErrInvalidCursor)
	})
}
