// DOMPurify は実 DOM が要る(SSR 中は fullText が常に null なので client project = 実ブラウザで足りる)。
// テストするのは Svelte コンポーネントではないが、DOM を要する都合で .svelte.spec.ts 命名にしている。
import { describe, expect, it } from 'vitest';
import { sanitizeArticleHtml } from './sanitize';

describe('sanitizeArticleHtml', () => {
	it('keeps structural tags that trafilatura may emit', () => {
		const html =
			'<h2>見出し</h2><p>本文<strong>強調</strong>と<em>斜体</em>。</p><ul><li>一</li><li>二</li></ul><pre><code>const x = 1;</code></pre><blockquote>引用</blockquote>';
		expect(sanitizeArticleHtml(html)).toBe(html);
	});

	it('strips script tags and event handler attributes', () => {
		const out = sanitizeArticleHtml('<p onclick="alert(1)">safe</p><script>alert(1)</script>');
		expect(out).not.toContain('<script');
		expect(out).not.toContain('onclick');
		expect(out).toContain('safe');
	});

	it('strips disallowed tags like iframe and style but keeps their text', () => {
		const out = sanitizeArticleHtml(
			'<iframe src="https://evil.example"></iframe><style>body{}</style>'
		);
		expect(out).not.toContain('<iframe');
		expect(out).not.toContain('<style');
	});

	it('drops javascript: hrefs', () => {
		const out = sanitizeArticleHtml('<a href="javascript:alert(1)">click</a>');
		expect(out).not.toContain('javascript:');
	});

	it('forces target=_blank and rel=noopener noreferrer on links', () => {
		const out = sanitizeArticleHtml('<a href="https://example.com">example</a>');
		expect(out).toContain('target="_blank"');
		expect(out).toContain('rel="noopener noreferrer"');
	});
});
