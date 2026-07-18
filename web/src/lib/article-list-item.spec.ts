import { describe, expect, it } from 'vitest';
import type { Article } from '$lib/api/schemas';
import { toArticleListItem } from './article-list-item';

const now = new Date('2026-07-18T12:00:00Z');

function article(overrides: Partial<Article> = {}): Article {
	return {
		id: 1,
		feed_id: 1,
		guid: 'urn:x:1',
		url: 'https://blog.example.com/entry/1',
		title: 'Entry one',
		content: '',
		published_at: '2026-07-18T11:15:00Z',
		created_at: '2026-07-18T11:20:00Z',
		feed_title: 'Example Blog',
		read: false,
		...overrides
	};
}

describe('toArticleListItem', () => {
	it('joins the feed title and the relative time as one quiet meta line', () => {
		const item = toArticleListItem(article(), now);
		expect(item.meta).toBe('Example Blog・45分前');
	});

	it('falls back to the article hostname when the feed title is missing', () => {
		const item = toArticleListItem(article({ feed_title: null }), now);
		expect(item.meta).toBe('blog.example.com・45分前');
	});

	it('falls back to created_at when published_at is missing', () => {
		const item = toArticleListItem(article({ published_at: null }), now);
		expect(item.meta).toBe('Example Blog・40分前');
	});

	it('omits the source entirely when neither feed title nor hostname is available', () => {
		const item = toArticleListItem(article({ feed_title: null, url: 'not a url' }), now);
		expect(item.meta).toBe('45分前');
	});

	it('carries the read fact through untouched (list dims, never counts)', () => {
		expect(toArticleListItem(article({ read: true }), now).read).toBe(true);
		expect(toArticleListItem(article({ read: false }), now).read).toBe(false);
	});
});
