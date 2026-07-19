import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import PasskeyList from './PasskeyList.svelte';
import {
	DELETE_CANCEL,
	DELETE_CONFIRM_LABEL,
	DELETE_LAST_PASSKEY_WARNING,
	DELETE_PASSKEY,
	DELETE_PASSKEY_WARNING,
	PASSKEY_NEVER_USED
} from '$lib/copy';
import type { Passkey } from '$lib/api/schemas';

const twoPasskeys: Passkey[] = [
	{ id: 1, created_at: '2026-07-01T09:00:00Z', last_used_at: '2026-07-10T09:00:00Z' },
	{ id: 2, created_at: '2026-07-02T09:00:00Z', last_used_at: null }
];

describe('PasskeyList.svelte', () => {
	it('lists each passkey with its registration date and last-used status', async () => {
		render(PasskeyList, { passkeys: twoPasskeys });

		await expect.element(page.getByText(/2026年7月1日/)).toBeVisible();
		await expect.element(page.getByText(/2026年7月10日/)).toBeVisible(); // 最終ログイン
		await expect.element(page.getByText(PASSKEY_NEVER_USED)).toBeVisible(); // 2本目は未使用
	});

	it('reveals the 警告様式 confirmation on 削除する click — nothing is deleted yet', async () => {
		const { container } = render(PasskeyList, { passkeys: twoPasskeys });

		await page.getByRole('button', { name: DELETE_PASSKEY }).first().click();

		const confirm = page.getByRole('region', { name: DELETE_CONFIRM_LABEL });
		await expect.element(confirm).toBeVisible();
		await expect.element(confirm).toHaveTextContent('注意:');
		await expect.element(confirm).toHaveTextContent(DELETE_PASSKEY_WARNING);
		// 2本あるうちの1本なので「最後の1本」警告は出ない
		await expect.element(confirm).not.toHaveTextContent(DELETE_LAST_PASSKEY_WARNING);

		const form = container.querySelector('form[action="/account?/delete"]');
		expect(form).not.toBeNull();
		expect(form?.querySelector('input[name="id"]')?.getAttribute('value')).toBe('1');
	});

	it('warns extra hard when deleting the last remaining passkey', async () => {
		render(PasskeyList, { passkeys: [twoPasskeys[0]] });

		await page.getByRole('button', { name: DELETE_PASSKEY }).click();

		const confirm = page.getByRole('region', { name: DELETE_CONFIRM_LABEL });
		await expect.element(confirm).toHaveTextContent(DELETE_LAST_PASSKEY_WARNING);
	});

	it('closes the confirmation with やめる without deleting anything', async () => {
		render(PasskeyList, { passkeys: twoPasskeys });

		await page.getByRole('button', { name: DELETE_PASSKEY }).first().click();
		await expect.element(page.getByRole('region', { name: DELETE_CONFIRM_LABEL })).toBeVisible();

		await page.getByRole('button', { name: DELETE_CANCEL }).click();

		expect(page.getByRole('region', { name: DELETE_CONFIRM_LABEL }).elements()).toHaveLength(0);
	});

	it('keeps only one confirmation open at a time', async () => {
		const { container } = render(PasskeyList, { passkeys: twoPasskeys });

		await page.getByRole('listitem').nth(0).getByRole('button', { name: DELETE_PASSKEY }).click();
		await page
			.getByRole('listitem')
			.nth(1)
			.getByRole('button', { name: DELETE_PASSKEY, exact: true })
			.click();

		expect(page.getByRole('region', { name: DELETE_CONFIRM_LABEL }).elements()).toHaveLength(1);
		expect(
			container
				.querySelector('form[action="/account?/delete"] input[name="id"]')
				?.getAttribute('value')
		).toBe('2');
	});
});
