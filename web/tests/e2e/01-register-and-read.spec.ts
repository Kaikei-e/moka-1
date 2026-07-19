import { test, expect } from '@playwright/test';

// フィード登録 → ホームの記事一覧 → 読書ビュー、のユーザージャーニー。
// 前提: フレッシュ DB + compose.e2e.yaml オーバーレイ(e2e/README.md)。
// フィクスチャ URL は moka-core が docker ネットワーク内で解決する(ブラウザは触らない)
const FIXTURE_URL = process.env.E2E_FIXTURE_URL ?? 'http://e2e-fixtures/feed.xml';

test.describe.configure({ mode: 'serial' });

test('空状態のホームからフィードを登録すると記事が並ぶ', async ({ page }) => {
	await page.goto('/');

	// 空状態は招待(§7.2)+ 登録導線
	await expect(page.getByText('まだ記事がありません。URL を貼るとここに並びます')).toBeVisible();
	await page.getByLabel('フィードの URL').fill(FIXTURE_URL);
	await page.getByRole('button', { name: '登録する' }).click();

	// 登録後はフィード管理に着地し、登録済みが見える。
	// フィード名はサイドバーの記事メタ行にも現れるため main(フィード一覧)にスコープする
	await expect(page.getByText('登録しました')).toBeVisible();
	await expect(page.getByRole('main').getByText('Moka E2E Fixture')).toBeVisible();

	// ホームに戻ると記事リスト(新しい順)が並ぶ。初期表示は無限スクロールの1ページ目(20件)
	await page.goto('/');
	const articleLinks = page.getByRole('link', { name: /article/ });
	await expect(articleLinks).toHaveCount(20);
	await expect(articleLinks.first()).toHaveText(/Third article/);
});

test('記事を選ぶと読書ビューが開き、moka の要約が運ばれてくる', async ({ page }) => {
	await page.goto('/');
	await page.getByRole('link', { name: 'Third article' }).click();

	// 本文(記事の声)
	await expect(page.getByRole('heading', { name: 'Third article' })).toBeVisible();

	// 既読の沈み: 開いた瞬間にサイドバーの行が楽観的に沈む(バッジ・数は出ない)
	await expect(page.getByRole('link', { name: 'Third article' })).toHaveAttribute(
		'data-read',
		'true'
	);

	// 要約カード: enrich.Scheduler(常駐エージェントループ)がフィード登録直後から
	// バックグラウンドで自動要約を回しているため、読書ビューを開いた時点で既に
	// 濃縮済み(テキストがそのまま出る)か、まだ未濃縮(明示ボタンが出る)かは
	// レースになる。存在と形をアサートし、生成内容の文言にもタイミングにも依存しない
	await expect(page.getByText('moka による要約')).toBeVisible();
	const summaryText = page.getByTestId('summary-text');
	const summarizeButton = page.getByRole('button', { name: '要約する' });
	await expect(summaryText.or(summarizeButton)).toBeVisible({ timeout: 20_000 });
	if (await summarizeButton.isVisible()) {
		await summarizeButton.click();
	}
	await expect(summaryText).toBeVisible({ timeout: 20_000 });
	await expect(summaryText).not.toHaveText('');

	// 対訳は LLM 翻訳の実装まで取り下げ中 — 読書ビューには現れない
	await expect(page.getByRole('button', { name: '対訳' })).not.toBeVisible();
	// 訊く(問い返し)の入力バーは読書ビューに常設(M2)
	await expect(page.getByPlaceholder('この記事について訊く…')).toBeVisible();
});

test('読書ビューで全文を取り寄せると本文が置き換わり、原文は新しいタブで開く', async ({ page }) => {
	await page.goto('/');
	await page.getByRole('link', { name: 'Third article' }).click();
	await expect(page.getByRole('heading', { name: 'Third article' })).toBeVisible();

	// 概要(RSS description)がまず表示されている
	await expect(page.getByText('Body of the third fixture article.')).toBeVisible();

	// 原文リンクは新しいタブで開く
	const originalLink = page.getByRole('link', { name: '原文を開く' });
	await expect(originalLink).toHaveAttribute('target', '_blank');
	await expect(originalLink).toHaveAttribute('rel', /noopener/);

	// 明示ボタンで取り寄せる。取得済みの全文が概要を置き換える
	await page.getByRole('button', { name: '全文を取り寄せる' }).click();
	await expect(page.getByText(/ninety-nine/)).toBeVisible();
	await expect(page.getByText('Body of the third fixture article.')).not.toBeVisible();

	// 取り寄せ後はボタンが消え、再取得は起きない(冪等)
	await expect(page.getByRole('button', { name: '全文を取り寄せる' })).not.toBeVisible();
});

test('フィード管理には登録済みフィードが一覧される', async ({ page }) => {
	await page.goto('/feeds');
	await expect(page.getByRole('heading', { name: 'フィード管理' })).toBeVisible();
	await expect(page.getByRole('main').getByText('Moka E2E Fixture')).toBeVisible();
});
