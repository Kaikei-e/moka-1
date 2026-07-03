package fulltext

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testArticleHTML = `<!DOCTYPE html>
<html lang="en">
<head><title>Third article — Moka E2E Fixture</title></head>
<body>
<nav><a href="/">Home</a> | <a href="/archive">Archive</a></nav>
<header><h1>Moka E2E Fixture</h1></header>
<article>
<h1>Third article</h1>
<p class="byline">By Moka E2E — 2026-07-01</p>
<p>This is the full text of the third fixture article, deliberately longer and more detailed than the RSS description that ships in the feed.</p>
<p>The second paragraph adds a concrete, deterministic sentence: the number ninety-nine appears here for assertions that need a stable anchor inside the extracted body.</p>
<p>A closing paragraph wraps up the third fixture article and confirms that paragraph boundaries survive extraction, and that boilerplate like navigation and footers is stripped away.</p>
</article>
<aside class="promo"><p>Subscribe to our newsletter for more fixtures like this one.</p></aside>
<footer><p>&copy; 2026 Moka E2E Fixture. All rights reserved.</p></footer>
</body>
</html>`

func TestTrafilaturaExtractorExtract(t *testing.T) {
	t.Parallel()

	t.Run("extracts the article body and drops boilerplate", func(t *testing.T) {
		t.Parallel()

		text, err := NewTrafilaturaExtractor().Extract([]byte(testArticleHTML), "http://example.com/articles/3")
		require.NoError(t, err)

		assert.Contains(t, text, "ninety-nine")
		assert.Contains(t, text, "boilerplate like navigation and footers is stripped away")
		assert.NotContains(t, text, "Subscribe to our newsletter", "サイドバーの販促は本文でない")
		assert.NotContains(t, text, "All rights reserved", "フッターは本文でない")
	})

	t.Run("preserves paragraph markup so the frontend's toParagraphs can split it", func(t *testing.T) {
		t.Parallel()

		text, err := NewTrafilaturaExtractor().Extract([]byte(testArticleHTML), "http://example.com/articles/3")
		require.NoError(t, err)

		assert.Regexp(t, `<p>[^<]*</p>\s*<p>`, text, "段落は <p> タグで区切られたまま返す")
	})

	t.Run("drops the leading heading so the title is not duplicated as a paragraph", func(t *testing.T) {
		t.Parallel()

		text, err := NewTrafilaturaExtractor().Extract([]byte(testArticleHTML), "http://example.com/articles/3")
		require.NoError(t, err)

		assert.NotContains(t, text, "<h1", "見出しは読書ビューのタイトルと重複するので落とす")
	})

	t.Run("empty document maps to ErrExtractFailed", func(t *testing.T) {
		t.Parallel()

		_, err := NewTrafilaturaExtractor().Extract([]byte("<html><body></body></html>"), "http://example.com/empty")
		require.ErrorIs(t, err, ErrExtractFailed)
	})

	t.Run("unparseable page url maps to ErrExtractFailed", func(t *testing.T) {
		t.Parallel()

		_, err := NewTrafilaturaExtractor().Extract([]byte(testArticleHTML), "http://[::1")
		require.ErrorIs(t, err, ErrExtractFailed)
	})
}
