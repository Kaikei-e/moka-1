package llm

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
		{
			// Qwen は "\n<think>" のように改行を先行させることがある — ストリーム側の
			// 判定(ThinkStreamStripper 相当)と同じく先頭空白は許容する
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

			text, stripped, closed := StripThink(tt.raw)
			assert.Equal(t, tt.wantText, text)
			assert.Equal(t, tt.wantStrip, stripped)
			assert.Equal(t, tt.wantClosed, closed)
		})
	}
}
