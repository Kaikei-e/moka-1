package llm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// feedAll は複数チャンクを ThinkStreamStripper に順に流し込み、都度の出力を連結する。
func feedAll(s *ThinkStreamStripper, chunks ...string) string {
	var out strings.Builder
	for _, c := range chunks {
		out.WriteString(s.Feed(c))
	}
	return out.String()
}

func TestThinkStreamStripper(t *testing.T) {
	t.Parallel()

	t.Run("no think tag: passes chunks straight through once undecided buffer resolves", func(t *testing.T) {
		t.Parallel()

		s := &ThinkStreamStripper{}
		got := feedAll(s, "記事の", "要約です。")
		flush, closed := s.Finish()
		got += flush

		assert.Equal(t, "記事の要約です。", got)
		assert.True(t, closed)
	})

	t.Run("very short output shorter than the open tag flushes on finish", func(t *testing.T) {
		t.Parallel()

		s := &ThinkStreamStripper{}
		got := feedAll(s, "短い")
		flush, closed := s.Finish()
		got += flush

		assert.Equal(t, "短い", got)
		assert.True(t, closed)
	})

	t.Run("closed think tag split across chunks: nothing leaks before close", func(t *testing.T) {
		t.Parallel()

		s := &ThinkStreamStripper{}
		got := feedAll(s, "<thi", "nk>推論", "過程</th", "ink>本文", "の続き")
		flush, closed := s.Finish()
		got += flush

		assert.Equal(t, "本文の続き", got)
		assert.True(t, closed)
	})

	t.Run("unclosed think tag: nothing is ever flushed and closed is false", func(t *testing.T) {
		t.Parallel()

		s := &ThinkStreamStripper{}
		got := feedAll(s, "<think>", "途中で切れた推論")
		flush, closed := s.Finish()
		got += flush

		assert.Empty(t, got)
		assert.False(t, closed)
	})

	t.Run("leading newline before think tag: CoT still never leaks", func(t *testing.T) {
		t.Parallel()

		// "\n<think>" で始まる応答(Qwen で頻出)。prefix 判定が先頭空白を許容しないと
		// passthrough に落ちて推論過程が丸ごとクライアントへ漏れる
		s := &ThinkStreamStripper{}
		got := feedAll(s, "\n<th", "ink>これは推論過程", "</think>", "本文の要約")
		flush, closed := s.Finish()
		got += flush

		assert.Equal(t, "本文の要約", got)
		assert.True(t, closed)
	})

	t.Run("whitespace-only first chunk stays buffered until decidable", func(t *testing.T) {
		t.Parallel()

		s := &ThinkStreamStripper{}
		got := feedAll(s, "\n", " ", "<think>推論</think>要約")
		flush, closed := s.Finish()
		got += flush

		assert.Equal(t, "要約", got)
		assert.True(t, closed)
	})

	t.Run("after passthrough begins, subsequent chunks stream immediately", func(t *testing.T) {
		t.Parallel()

		s := &ThinkStreamStripper{}
		first := feedAll(s, "本文冒頭これは十分に長い")
		second := s.Feed("続き")
		flush, closed := s.Finish()

		assert.Equal(t, "本文冒頭これは十分に長い", first)
		assert.Equal(t, "続き", second)
		assert.Empty(t, flush)
		assert.True(t, closed)
	})
}
