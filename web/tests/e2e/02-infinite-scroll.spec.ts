import { test, expect } from '@playwright/test';

// サイドバー(記事リスト = 左カラム)の無限スクロール: 初期20件 → 末尾スクロールで25件 →
// 終端(next_cursor: null)で静かに止まる。前提: フレッシュ DB + compose.e2e.yaml オーバーレイ
// (e2e/README.md)。フィクスチャは25件(guid urn:moka-e2e:1-25、e2e/fixtures/feed.xml)
const FIXTURE_URL = process.env.E2E_FIXTURE_URL ?? 'http://e2e-fixtures/feed.xml';

test.describe.configure({ mode: 'serial' });

test('サイドバーを最下部までスクロールすると次のページが読み込まれ、終端で静かに止まる', async ({
	page
}) => {
	// フィード管理から登録する(常設フォームなので、他 spec が先に同じフィードを
	// 登録済みでも冪等に安全 — ホームの空状態フォームには依存しない)。
	// 登録済み状態ではサイドバーにも同じフォームが常設されるため main 側に絞る
	await page.goto('/feeds');
	const main = page.getByRole('main');
	await main.getByLabel('フィードの URL').fill(FIXTURE_URL);
	await main.getByRole('button', { name: '登録する' }).click();
	await expect(page.getByText('登録しました')).toBeVisible();

	await page.goto('/');
	const articleLinks = page.getByRole('link', { name: /article/ });
	await expect(articleLinks).toHaveCount(20);
	await expect(articleLinks.first()).toHaveText(/Third article/);

	// センチネルをビューポートに入れる → IntersectionObserver が発火し次ページを取得する
	await page.getByTestId('article-list-sentinel').scrollIntoViewIfNeeded();
	await expect(articleLinks).toHaveCount(25);
	await expect(articleLinks.last()).toHaveText(/Archive article 25/);

	// 終端に達したら追加のフェッチは起きず、ローディング表示もセンチネルも残らない
	await expect(page.getByText('続きを読み込んでいます')).not.toBeVisible();
	await expect(page.getByTestId('article-list-sentinel')).not.toBeAttached();
});
