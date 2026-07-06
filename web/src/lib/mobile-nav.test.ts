import { describe, expect, it } from 'vitest';
import { shouldResetReadingScroll, topbarMode } from './mobile-nav';

// モバイル topbar の状態判定(DESIGN_LANGUAGE.md §4.3 v3.2.0): ルートが `/`(記事リスト)か、
// それ以外(読書ビュー・フィード管理)かの2分岐のみ。history には依存しない純関数。
describe('topbarMode', () => {
	it('shows the article list at home', () => {
		expect(topbarMode('/')).toBe('list');
	});

	it('shows back for the reading view', () => {
		expect(topbarMode('/articles/7')).toBe('back');
	});

	it('shows back for feed management', () => {
		expect(topbarMode('/feeds')).toBe('back');
	});
});

// 読書ペイン(.reading)は独立スクロールコンテナで、SvelteKit は window のスクロールしか
// リセットしない。パスが変わる遷移でだけ自前で先頭に戻す判定の純関数。
describe('shouldResetReadingScroll', () => {
	it('resets when navigating between different articles', () => {
		expect(shouldResetReadingScroll('/articles/7', '/articles/8')).toBe(true);
	});

	it('resets when entering an article from the list', () => {
		expect(shouldResetReadingScroll('/', '/articles/7')).toBe(true);
	});

	it('keeps the position when the path did not change (hash / query only)', () => {
		expect(shouldResetReadingScroll('/articles/7', '/articles/7')).toBe(false);
	});

	it('keeps the position when the destination is unknown', () => {
		expect(shouldResetReadingScroll('/articles/7', null)).toBe(false);
	});
});
