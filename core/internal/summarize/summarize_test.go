package summarize

import (
	"strings"
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
		{
			// Qwen は "\n<think>" のように改行を先行させることがある — ストリーム側の
			// 判定(thinkStreamStripper)と同じく先頭空白は許容する
			name:       "leading whitespace before think tag is tolerated",
			raw:        "\n <think>推論</think>要約本文",
			wantText:   "要約本文",
			wantStrip:  true,
			wantClosed: true,
		},
		{
			// 冒頭以外の <think> は本文(引用等)とみなす — ストリームで見えた本文と
			// 保存本文が食い違わないための prefix 限定判定
			name:       "mid-text think tag is kept as content",
			raw:        "本文の前半<think>これも本文</think>後半",
			wantText:   "本文の前半<think>これも本文</think>後半",
			wantStrip:  false,
			wantClosed: true,
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
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(s.feed(c))
	}
	return out.String()
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

	t.Run("leading newline before think tag: CoT still never leaks", func(t *testing.T) {
		t.Parallel()

		// "\n<think>" で始まる応答(Qwen で頻出)。prefix 判定が先頭空白を許容しないと
		// passthrough に落ちて推論過程が丸ごとクライアントへ漏れる
		s := &thinkStreamStripper{}
		got := feedAll(s, "\n<th", "ink>これは推論過程", "</think>", "本文の要約")
		flush, closed := s.finish()
		got += flush

		assert.Equal(t, "本文の要約", got)
		assert.True(t, closed)
	})

	t.Run("whitespace-only first chunk stays buffered until decidable", func(t *testing.T) {
		t.Parallel()

		s := &thinkStreamStripper{}
		got := feedAll(s, "\n", " ", "<think>推論</think>要約")
		flush, closed := s.finish()
		got += flush

		assert.Equal(t, "要約", got)
		assert.True(t, closed)
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
