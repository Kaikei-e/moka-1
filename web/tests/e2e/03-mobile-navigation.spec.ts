import { test, expect } from '@playwright/test';

// モバイル(< 900px)のナビゲーション: ドロワーを廃止し、記事リスト(タイムライン)⇄読書ビューの
// マスター・ディテール式プッシュ遷移に統一する(DESIGN_LANGUAGE.md §4.3 v3.2.0)。
// 前提: フレッシュ DB + compose.e2e.yaml オーバーレイ(e2e/README.md)。playwright.config.ts の
// mobile プロジェクト(viewport 390x844)でのみ実行される。
const FIXTURE_URL = process.env.E2E_FIXTURE_URL ?? 'http://e2e-fixtures/feed.xml';

test.describe.configure({ mode: 'serial' });

async function registerFixtureFeed(page: import('@playwright/test').Page) {
	// 他 spec が先に同じフィードを登録済みでも冪等に安全(02-infinite-scroll.spec.ts と同じ作法)
	await page.goto('/feeds');
	const main = page.getByRole('main');
	await main.getByLabel('フィードの URL').fill(FIXTURE_URL);
	await main.getByRole('button', { name: '登録する' }).click();
	await expect(page.getByText('登録しました')).toBeVisible();
}

test('モバイルでホームを開くと記事リストがフル画面で見え、読書カラムの空状態は隠れている', async ({
	page
}) => {
	await registerFixtureFeed(page);

	await page.goto('/');
	const articleLinks = page.getByRole('link', { name: /article/ });
	await expect(articleLinks).toHaveCount(20);
	await expect(page.getByText('一覧から記事を選ぶと、ここに運ばれます')).not.toBeVisible();
});

test('記事をタップすると読書ビューに遷移し、topbarが「← 戻る」になる', async ({ page }) => {
	await page.goto('/');

	await expect(page.getByRole('link', { name: 'moka-1' })).toBeVisible();
	await page.getByRole('link', { name: 'Third article' }).click();

	await expect(page.getByRole('heading', { name: 'Third article' })).toBeVisible();
	await expect(page.getByRole('link', { name: '← 戻る' })).toBeVisible();
	await expect(page.getByRole('link', { name: 'moka-1' })).not.toBeVisible();
});

test('「← 戻る」で記事リストへ戻ると、追加読み込み済みのページがそのまま残っている', async ({
	page
}) => {
	await page.goto('/');
	const articleLinks = page.getByRole('link', { name: /article/ });
	await expect(articleLinks).toHaveCount(20);

	// センチネルをビューポートに入れて次ページ(25件目まで)を読み込ませる
	await page.getByTestId('article-list-sentinel').scrollIntoViewIfNeeded();
	await expect(articleLinks).toHaveCount(25);

	await page.getByRole('link', { name: 'Third article' }).click();
	await expect(page.getByRole('heading', { name: 'Third article' })).toBeVisible();

	await page.getByRole('link', { name: '← 戻る' }).click();

	// 一覧に戻ってもリセットされず25件のまま(ArticleList はアンマウントされていない)
	await expect(articleLinks).toHaveCount(25);
});

test('別の記事を開くと読書ビューのスクロール位置が先頭に戻る', async ({ page }) => {
	// .reading(<main>)は position:fixed の独立スクロールコンテナで、SvelteKit の
	// ナビゲーションでは window しかリセットされない — レイアウト側の afterNavigate で戻す
	await page.goto('/');
	await page.getByRole('link', { name: 'Third article' }).click();
	await expect(page.getByRole('heading', { name: 'Third article' })).toBeVisible();

	const reading = page.getByRole('main');
	await reading.evaluate((el) => el.scrollTo(0, el.scrollHeight));
	await expect.poll(() => reading.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);

	await page.getByRole('link', { name: '← 戻る' }).click();
	await page.getByRole('link', { name: 'Second article' }).click();
	await expect(page.getByRole('heading', { name: 'Second article' })).toBeVisible();

	// 前の記事のスクロール位置を引き継がない
	await expect.poll(() => reading.evaluate((el) => el.scrollTop)).toBe(0);
});

test('topbarの「…」メニューからフィード管理へ遷移でき、topbarが「← 戻る」になる', async ({
	page
}) => {
	await page.goto('/');

	await page.getByRole('button', { name: 'メニュー' }).click();
	await page.getByRole('menuitem', { name: 'フィード管理' }).click();

	await expect(page.getByRole('heading', { name: 'フィード管理' })).toBeVisible();
	await expect(page.getByRole('link', { name: '← 戻る' })).toBeVisible();
});
