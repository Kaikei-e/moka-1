// モバイル topbar の状態(DESIGN_LANGUAGE.md §4.3 v3.2.0)。
// `/` = 記事リストを主役に表示、それ以外(読書ビュー・フィード管理)は「← 戻る」を表示する。
export type TopbarMode = 'list' | 'back';

export function topbarMode(pathname: string): TopbarMode {
	return pathname === '/' ? 'list' : 'back';
}

// SvelteKit がナビゲーションで戻すのは window のスクロールのみ。モバイル(< 900px)では
// .reading が position:fixed の独立スクロールコンテナなので、パスが変わった遷移では
// 自前で先頭に戻す(同一パス内のハッシュ移動等ではスクロールを保つ)
export function shouldResetReadingScroll(from: string | null, to: string | null): boolean {
	return to !== null && from !== to;
}
