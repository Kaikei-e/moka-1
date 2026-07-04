// モバイル topbar の状態(DESIGN_LANGUAGE.md §4.3 v3.2.0)。
// `/` = 記事リストを主役に表示、それ以外(読書ビュー・フィード管理)は「← 戻る」を表示する。
export type TopbarMode = 'list' | 'back';

export function topbarMode(pathname: string): TopbarMode {
	return pathname === '/' ? 'list' : 'back';
}
