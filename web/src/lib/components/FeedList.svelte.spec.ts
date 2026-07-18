import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import FeedList from './FeedList.svelte';
import { DELETE_CANCEL, DELETE_CONFIRM_LABEL, DELETE_FEED, DELETE_FEED_WARNING } from '$lib/copy';
import type { Feed } from '$lib/api/schemas';

const feeds: Feed[] = [
	{
		id: 1,
		url: 'https://example.com/feed.xml',
		title: 'Example Feed',
		created_at: '2026-07-01T09:00:00Z'
	},
	{
		id: 2,
		url: 'https://blog.example.com/rss',
		title: 'Second Feed',
		created_at: '2026-07-02T09:00:00Z'
	}
];

describe('FeedList.svelte', () => {
	it('lists each feed with its url and registration date', async () => {
		render(FeedList, { feeds });

		await expect.element(page.getByText('Example Feed')).toBeVisible();
		await expect.element(page.getByText(/2026年7月1日 に登録/)).toBeVisible();
	});

	it('reveals the 警告様式 confirmation on the first 削除する click — nothing is deleted yet', async () => {
		const { container } = render(FeedList, { feeds });

		await page.getByRole('button', { name: DELETE_FEED }).first().click();

		const confirm = page.getByRole('region', { name: DELETE_CONFIRM_LABEL });
		await expect.element(confirm).toBeVisible();
		// 「注意:」接頭辞 + 記事も消える事実の明示(§2.4 / ADR00019)
		await expect.element(confirm).toHaveTextContent('注意:');
		await expect.element(confirm).toHaveTextContent(DELETE_FEED_WARNING);
		// この時点ではフォーム送信は起きていない — フィードは残ったまま
		await expect.element(page.getByText('Example Feed')).toBeVisible();
		// 実削除は named action への native POST(登録フォームと同じ素朴な経路)
		const form = container.querySelector('form[action="/feeds?/delete"]');
		expect(form).not.toBeNull();
		expect(form?.querySelector('input[name="id"]')?.getAttribute('value')).toBe('1');
	});

	it('closes the confirmation with やめる without deleting anything', async () => {
		render(FeedList, { feeds });

		await page.getByRole('button', { name: DELETE_FEED }).first().click();
		await expect.element(page.getByRole('region', { name: DELETE_CONFIRM_LABEL })).toBeVisible();

		await page.getByRole('button', { name: DELETE_CANCEL }).click();

		expect(page.getByRole('region', { name: DELETE_CONFIRM_LABEL }).elements()).toHaveLength(0);
		await expect.element(page.getByText('Example Feed')).toBeVisible();
	});

	it('keeps only one confirmation open at a time (迷いを増やさない)', async () => {
		const { container } = render(FeedList, { feeds });

		await page.getByRole('listitem').nth(0).getByRole('button', { name: DELETE_FEED }).click();
		await page
			.getByRole('listitem')
			.nth(1)
			.getByRole('button', { name: DELETE_FEED, exact: true })
			.click();

		expect(page.getByRole('region', { name: DELETE_CONFIRM_LABEL }).elements()).toHaveLength(1);
		expect(
			container
				.querySelector('form[action="/feeds?/delete"] input[name="id"]')
				?.getAttribute('value')
		).toBe('2');
	});
});
