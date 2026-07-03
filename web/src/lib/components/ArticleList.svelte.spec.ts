import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import ArticleList from './ArticleList.svelte';
import { EMPTY_ARTICLES } from '$lib/copy';
import type { Article } from '$lib/api/schemas';

const articles: Article[] = [
	{
		id: 2,
		feed_id: 1,
		guid: 'urn:x:2',
		url: 'https://example.com/2',
		title: 'Newest',
		content: '',
		published_at: '2026-07-02T09:00:00Z',
		created_at: '2026-07-02T09:00:00Z'
	},
	{
		id: 1,
		feed_id: 1,
		guid: 'urn:x:1',
		url: 'https://example.com/1',
		title: 'Older',
		content: '',
		published_at: '2026-07-01T09:00:00Z',
		created_at: '2026-07-01T09:00:00Z'
	}
];

describe('ArticleList.svelte', () => {
	it('links each article to its reading view', async () => {
		render(ArticleList, { articles, currentId: null });

		const link = page.getByRole('link', { name: /Newest/ });
		await expect.element(link).toHaveAttribute('href', '/articles/2');
	});

	it('marks the article being read with aria-current', async () => {
		render(ArticleList, { articles, currentId: 1 });

		await expect
			.element(page.getByRole('link', { name: /Older/ }))
			.toHaveAttribute('aria-current', 'page');
	});

	it('invites instead of apologizing when there are no articles', async () => {
		render(ArticleList, { articles: [], currentId: null });

		await expect.element(page.getByText(EMPTY_ARTICLES)).toBeInTheDocument();
	});
});
