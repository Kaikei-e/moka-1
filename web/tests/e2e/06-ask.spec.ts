import { test, expect } from '@playwright/test';

// 訊くバー(問い返し Q&A、M2): 読書ビューの入力バー → BFF /articles/[id]/qa →
// moka-core の SSE(sources → delta → done)を逐次読み、回答がフラットなブロックに据わる。
// 回答文はモック LLM 由来 — 存在と形をアサートし、内容の文言はアサートしない。
// llm 停止時の error イベント(静かな失敗文言)はサーバ側を rag_failsoft_e2e.sh が守り、
// UI 側の失敗表示はコンポーネントテスト(AskBar.svelte.spec.ts)が守る。
// 前提: フレッシュ DB + compose.e2e.yaml オーバーレイ(e2e/README.md)
const FIXTURE_URL = process.env.E2E_FIXTURE_URL ?? 'http://e2e-fixtures/feed.xml';

test.describe.configure({ mode: 'serial' });

test('読書ビューで訊くと、質問が積まれ回答が届いて据わる', async ({ page }) => {
	// 登録(冪等)
	await page.goto('/feeds');
	const main = page.getByRole('main');
	await main.getByLabel('フィードの URL').fill(FIXTURE_URL);
	await main.getByRole('button', { name: '登録する' }).click();
	await expect(page.getByText('登録しました')).toBeVisible();

	// 読書ビューへ
	await page.goto('/');
	await page.getByRole('link', { name: 'Third article' }).click();
	await expect(page.getByRole('heading', { name: 'Third article' })).toBeVisible();

	// 訊くバーは常設。質問を入れて送る
	const askInput = page.getByLabel('この記事について訊く');
	await askInput.fill('この記事の要点を教えてください');
	await page.getByRole('button', { name: '訊く' }).click();

	// 質問がスタックに積まれ、下書きは消える
	await expect(page.getByText('この記事の要点を教えてください')).toBeVisible();
	await expect(askInput).toHaveValue('');

	// 回答ブロックが届く(SSE delta → done)。文言はアサートしない
	const answer = page.getByTestId('qa-answer');
	await expect(answer).toBeVisible({ timeout: 20_000 });
	await expect(answer).not.toHaveText('');

	// 静かな給仕: エラーブロック(role=alert)は出ない
	await expect(page.getByRole('alert')).toHaveCount(0);
});
