import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import SummaryCard from './SummaryCard.svelte';
import { SUMMARY_PENDING } from '$lib/copy';

describe('SummaryCard.svelte', () => {
	it('is labeled as the voice of moka and shows the pending drip (no fake summary)', async () => {
		render(SummaryCard);

		await expect.element(page.getByText('moka による要約')).toBeInTheDocument();
		await expect.element(page.getByTestId('summary-drip')).toBeInTheDocument();
		await expect.element(page.getByText(SUMMARY_PENDING)).toBeInTheDocument();
	});
});
