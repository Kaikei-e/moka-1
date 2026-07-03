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

	// 登録後はフィード管理に着地し、登録済みが見える
	await expect(page.getByText('登録しました')).toBeVisible();
	await expect(page.getByText('Moka E2E Fixture')).toBeVisible();

	// ホームに戻ると記事リスト(新しい順)が並ぶ
	await page.goto('/');
	const articleLinks = page.getByRole('link', { name: /article/ });
	await expect(articleLinks).toHaveCount(3);
	await expect(articleLinks.first()).toHaveText(/Third article/);
});

test('記事を選ぶと読書ビューが開き、AI 要素は準備中の給仕として現れる', async ({ page }) => {
	await page.goto('/');
	await page.getByRole('link', { name: 'Third article' }).click();

	// 本文(記事の声)
	await expect(page.getByRole('heading', { name: 'Third article' })).toBeVisible();

	// 要約カード: 存在と形をアサートし、生成内容の文言には依存しない
	await expect(page.getByText('moka による要約')).toBeVisible();
	await expect(page.getByTestId('summary-drip')).toBeVisible();

	// Q&A 入力バー
	await expect(page.getByPlaceholder('この記事について訊く…')).toBeVisible();

	// 対訳へ切り替えると未訳段落のドリップが段落位置に置かれる(§5.3)
	await page.getByRole('button', { name: '対訳' }).click();
	await expect(page.getByTestId('untranslated-drip').first()).toBeVisible();
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
	await expect(page.getByText('Moka E2E Fixture')).toBeVisible();
});
