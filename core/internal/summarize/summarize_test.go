package summarize

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripThink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		raw        string
		wantText   string
		wantStrip  bool
		wantClosed bool
	}{
		{
			name:       "no think tag passes through trimmed",
			raw:        "  記事の要約です。  ",
			wantText:   "記事の要約です。",
			wantStrip:  false,
			wantClosed: true,
		},
		{
			name:       "closed think tag is stripped, only the answer remains",
			raw:        "<think>これは推論過程</think>\n記事の要約です。",
			wantText:   "記事の要約です。",
			wantStrip:  true,
			wantClosed: true,
		},
		{
			name:       "unclosed think tag (truncated) yields empty text",
			raw:        "<think>途中で切れた推論過程がここまで続く",
			wantText:   "",
			wantStrip:  true,
			wantClosed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			text, stripped, closed := stripThink(tt.raw)
			assert.Equal(t, tt.wantText, text)
			assert.Equal(t, tt.wantStrip, stripped)
			assert.Equal(t, tt.wantClosed, closed)
		})
	}
}
