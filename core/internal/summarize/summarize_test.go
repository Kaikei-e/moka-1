package summarize

// thinkStreamStripper のテストは llm.ThinkStreamStripper への移設(rag との共用化)に伴い
// internal/llm/thinkstream_test.go へ移動した。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEstimateTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"ascii is 4 chars per token", strings.Repeat("a", 40), 10},
		{"japanese is 2 tokens per char (byte count ではなく rune count)", "日本語テスト", 12},
		{"mixed ascii and japanese", "abcd日本", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, estimateTokens(tt.text))
		})
	}
}
