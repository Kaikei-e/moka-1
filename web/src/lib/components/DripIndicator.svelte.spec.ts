import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import DripIndicator from './DripIndicator.svelte';

describe('DripIndicator.svelte', () => {
	it('announces its label as a status (drip is decoration, text is truth)', async () => {
		render(DripIndicator, { label: '蒸らしています' });

		await expect.element(page.getByRole('status')).toHaveTextContent('蒸らしています');
	});

	it('exposes an optional testid for e2e anchoring', async () => {
		render(DripIndicator, { label: 'まだ訳されていません', testid: 'untranslated-drip' });

		await expect.element(page.getByTestId('untranslated-drip')).toBeInTheDocument();
	});

	it('marks completion via data-completed without dropping the label (推論完了専用の金の一滴)', async () => {
		render(DripIndicator, { label: '準備ができました', testid: 'done-drip', completed: true });

		const el = page.getByTestId('done-drip');
		await expect.element(el).toHaveTextContent('準備ができました');
		await expect.element(el).toHaveAttribute('data-completed', 'true');
	});

	it('defaults to not completed', async () => {
		render(DripIndicator, { label: '注いでいます', testid: 'pending-drip' });

		await expect.element(page.getByTestId('pending-drip')).not.toHaveAttribute('data-completed');
	});
});
