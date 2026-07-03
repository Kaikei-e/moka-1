// フィード由来の content(外部入力)をプレーンな段落列に落とす。
// {@html} は使わない前提 — タグはここですべて剥がし、XSS の経路を作らない

const DROP_ELEMENTS = /<(script|style)[\s\S]*?<\/\1>/gi;
const BLOCK_BREAKS = /<\/(?:p|div|h[1-6]|li|blockquote|pre)>|<br\s*\/?>/gi;
const ANY_TAG = /<[^>]*>/g;

function decodeEntities(s: string): string {
	return s
		.replace(/&lt;/g, '<')
		.replace(/&gt;/g, '>')
		.replace(/&quot;/g, '"')
		.replace(/&#39;/g, "'")
		.replace(/&nbsp;/g, ' ')
		.replace(/&amp;/g, '&'); // 二重デコードを避けるため最後
}

export function toParagraphs(content: string): string[] {
	const text = decodeEntities(
		content.replace(DROP_ELEMENTS, '').replace(BLOCK_BREAKS, '\n\n').replace(ANY_TAG, '')
	);
	return text
		.split(/\n\s*\n/)
		.map((p) => p.replace(/\s+/g, ' ').trim())
		.filter(Boolean);
}
