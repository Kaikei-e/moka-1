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

// feedAll は複数チャンクを thinkStreamStripper に順に流し込み、都度の出力を連結する。
func feedAll(s *thinkStreamStripper, chunks ...string) string {
	var out string
	for _, c := range chunks {
		out += s.feed(c)
	}
	return out
}

func TestThinkStreamStripper(t *testing.T) {
	t.Parallel()

	t.Run("no think tag: passes chunks straight through once undecided buffer resolves", func(t *testing.T) {
		t.Parallel()

		s := &thinkStreamStripper{}
		got := feedAll(s, "記事の", "要約です。")
		flush, closed := s.finish()
		got += flush

		assert.Equal(t, "記事の要約です。", got)
		assert.True(t, closed)
	})

	t.Run("very short output shorter than the open tag flushes on finish", func(t *testing.T) {
		t.Parallel()

		s := &thinkStreamStripper{}
		got := feedAll(s, "短い")
		flush, closed := s.finish()
		got += flush

		assert.Equal(t, "短い", got)
		assert.True(t, closed)
	})

	t.Run("closed think tag split across chunks: nothing leaks before close", func(t *testing.T) {
		t.Parallel()

		s := &thinkStreamStripper{}
		got := feedAll(s, "<thi", "nk>推論", "過程</th", "ink>本文", "の続き")
		flush, closed := s.finish()
		got += flush

		assert.Equal(t, "本文の続き", got)
		assert.True(t, closed)
	})

	t.Run("unclosed think tag: nothing is ever flushed and closed is false", func(t *testing.T) {
		t.Parallel()

		s := &thinkStreamStripper{}
		got := feedAll(s, "<think>", "途中で切れた推論")
		flush, closed := s.finish()
		got += flush

		assert.Empty(t, got)
		assert.False(t, closed)
	})

	t.Run("after passthrough begins, subsequent chunks stream immediately", func(t *testing.T) {
		t.Parallel()

		s := &thinkStreamStripper{}
		first := feedAll(s, "本文冒頭これは十分に長い")
		second := s.feed("続き")
		flush, closed := s.finish()

		assert.Equal(t, "本文冒頭これは十分に長い", first)
		assert.Equal(t, "続き", second)
		assert.Empty(t, flush)
		assert.True(t, closed)
	})
}
