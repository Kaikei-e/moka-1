import { test, expect } from '@playwright/test';

// フィードの削除(店との別れ、ADR00019): 一覧の削除ボタン → 警告様式の二段確認 →
// named action(?/delete)への native POST → CASCADE で記事ごと消える。
// ファイル名の 04 は意図的 — 直列実行の最後に走り、他 spec が使うフィクスチャフィードを
// 消しても波及しない。前提: フレッシュ DB + compose.e2e.yaml オーバーレイ(e2e/README.md)
const FIXTURE_URL = process.env.E2E_FIXTURE_URL ?? 'http://e2e-fixtures/feed.xml';

test.describe.configure({ mode: 'serial' });

test('フィードを削除すると二段確認を経て、その店の記事ごと消える', async ({ page }) => {
	// 登録(冪等 — 他 spec が先に登録済みでも安全)
	await page.goto('/feeds');
	const main = page.getByRole('main');
	await main.getByLabel('フィードの URL').fill(FIXTURE_URL);
	await main.getByRole('button', { name: '登録する' }).click();
	// フィード名はサイドバーの記事メタ行にも現れるため main(フィード一覧)にスコープする
	await expect(page.getByText('登録しました')).toBeVisible();
	await expect(main.getByText('Moka E2E Fixture')).toBeVisible();

	// 一段目: 削除ボタンで警告様式の確認が現れる(まだ何も消えない)
	await main.getByRole('button', { name: '削除する' }).first().click();
	const confirm = page.getByRole('region', { name: '削除の確認' });
	await expect(confirm).toBeVisible();
	await expect(confirm).toContainText('注意:');
	await expect(confirm).toContainText('記事・要約・既読の記録もすべて消えます');

	// やめると確認は閉じ、フィードは残る
	await page.getByRole('button', { name: 'やめる' }).click();
	await expect(confirm).not.toBeVisible();
	await expect(main.getByText('Moka E2E Fixture')).toBeVisible();

	// 二段目: 確認のうえ削除する → redirect-after-POST で戻り、トーストが出る
	await main.getByRole('button', { name: '削除する' }).first().click();
	await confirm.getByRole('button', { name: '削除する' }).click();

	await expect(page.getByText('削除しました')).toBeVisible();
	await expect(main.getByText('Moka E2E Fixture')).not.toBeVisible();
	await expect(
		page.getByText('まだフィードがありません。URL を貼るとここに並びます')
	).toBeVisible();

	// CASCADE: その店の記事もホームから消えている(空状態の招待に戻る)
	await page.goto('/');
	await expect(page.getByText('まだ記事がありません。URL を貼るとここに並びます')).toBeVisible();
});
