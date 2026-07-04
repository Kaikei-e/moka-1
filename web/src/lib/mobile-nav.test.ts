import { describe, expect, it } from 'vitest';
import { topbarMode } from './mobile-nav';

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
