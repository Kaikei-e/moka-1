import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import AskBar from './AskBar.svelte';
import { ANSWER_PENDING } from '$lib/copy';

describe('AskBar.svelte', () => {
	it('stacks the question and a pending matcha block instead of inventing an answer', async () => {
		render(AskBar);

		const input = page.getByPlaceholder('この記事について訊く…');
		await input.fill('この記事の要点は');
		await page.getByRole('button', { name: '訊く' }).click();

		await expect.element(page.getByText('この記事の要点は')).toBeInTheDocument();
		await expect.element(page.getByText(ANSWER_PENDING)).toBeInTheDocument();
	});

	it('ignores empty questions', async () => {
		render(AskBar);

		await page.getByRole('button', { name: '訊く' }).click();

		await expect.element(page.getByPlaceholder('この記事について訊く…')).toBeInTheDocument();
		expect(page.getByText(ANSWER_PENDING).elements()).toHaveLength(0);
	});
});
