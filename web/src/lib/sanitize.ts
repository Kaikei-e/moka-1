// 取り寄せた全文(fulltext.Text — trafilatura が返す段落構造つき HTML)を安全に描画するための
// サニタイズ。SSR では fullText は常に null(クライアント起点フェッチでしか埋まらない)なので、
// DOMPurify の実行はブラウザ内に限られる — jsdom 等の SSR 対応は不要。
import DOMPurify from 'dompurify';

const ALLOWED_TAGS = [
	'h2',
	'h3',
	'h4',
	'h5',
	'h6',
	'p',
	'ul',
	'ol',
	'li',
	'pre',
	'code',
	'strong',
	'em',
	'a',
	'blockquote',
	'br'
];
const ALLOWED_ATTR = ['href'];

let hookRegistered = false;

function ensureExternalLinkHook() {
	if (hookRegistered) return;
	hookRegistered = true;
	// 全リンクを新しいタブで開く(既存の「原文を開く」リンクと同じ作法)
	DOMPurify.addHook('afterSanitizeAttributes', (node) => {
		if (node.tagName === 'A') {
			node.setAttribute('target', '_blank');
			node.setAttribute('rel', 'noopener noreferrer');
		}
	});
}

export function sanitizeArticleHtml(html: string): string {
	ensureExternalLinkHook();
	return DOMPurify.sanitize(html, { ALLOWED_TAGS, ALLOWED_ATTR });
}
