// フィード由来の URL は信用しない(javascript:/data: 等が保存されうる)。
// href として描画してよいのは絶対 URL の http/https に限る — core 側の正規化とは独立した web 側の防御
export function isSafeExternalUrl(url: string): boolean {
	let parsed: URL;
	try {
		parsed = new URL(url);
	} catch {
		return false;
	}
	return parsed.protocol === 'http:' || parsed.protocol === 'https:';
}

// 記事 URL からホスト名を取り出す(フィード名が無いときのリスト行の代替表示)。
// 表示専用 — href には使わないので http(s) 以外も許すが、解釈できなければ null
export function hostnameOf(url: string): string | null {
	try {
		return new URL(url).hostname || null;
	} catch {
		return null;
	}
}
