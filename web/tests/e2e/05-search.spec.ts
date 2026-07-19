import { test, expect } from '@playwright/test';

// サイドバー検索(M2、ADR00022): 入力デバウンス → BFF /search → moka-core のハイブリッド検索。
// e2e-llm-mock は /embeddings(決定的な feature hashing)も実装しており、enrich.Scheduler の
// 自動埋め込みが済めばベクトル側(pgvector 近傍)も RRF に効く — テキスト一致ゼロの語でも
// 近傍の記事が返る。検索中は通常一覧が隠れ、空クエリで通常一覧へ戻る。
// 前提: フレッシュ DB + compose.e2e.yaml オーバーレイ(e2e/README.md)
const FIXTURE_URL = process.env.E2E_FIXTURE_URL ?? 'http://e2e-fixtures/feed.xml';

test.describe.configure({ mode: 'serial' });

test('検索語で結果が並び、無関係な語もベクトル近傍が差し出され、消すと通常一覧へ戻る', async ({
	page
}) => {
	// 登録(冪等 — 他 spec が先に登録済みでも安全。04 が削除した後でも再登録される)
	await page.goto('/feeds');
	const main = page.getByRole('main');
	await main.getByLabel('フィードの URL').fill(FIXTURE_URL);
	await main.getByRole('button', { name: '登録する' }).click();
	await expect(page.getByText('登録しました')).toBeVisible();

	await page.goto('/');
	const searchBox = page.getByRole('searchbox', { name: '記事を探す' });
	await expect(searchBox).toBeVisible();

	// ヒットあり: 結果はリスト行と同じ手がかり(タイトル + メタ)で「検索結果」ナビに並ぶ
	await searchBox.fill('Third article');
	const results = page.getByRole('navigation', { name: '検索結果' });
	await expect(results.getByRole('link', { name: 'Third article' })).toBeVisible();

	// 検索中は通常一覧(無限スクロールのリスト)が隠れている
	await expect(page.getByTestId('article-list-sentinel')).not.toBeVisible();

	// 結果から読書ビューへ入れる
	await results.getByRole('link', { name: 'Third article' }).click();
	await expect(page.getByRole('heading', { name: 'Third article' })).toBeVisible();

	// 無関係な語でもベクトル側(埋め込み済み記事の cosine 近傍)がヒットを返す —
	// テキスト一致ゼロなので、これは純粋にベクトル側由来。enrich.Scheduler の自動埋め込みが
	// 済むまで BFF をポーリングで待ってから UI を検証する(埋め込み前は 0 件でレースする)。
	// なお top-k 近傍検索に閾値は無いため、埋め込みが存在する限り「見つかりませんでした」の
	// 空状態は現れない — 空状態 UI 自体は ArticleSearch.svelte.spec.ts(コンポーネント)が守る
	await expect
		.poll(
			async () => {
				const res = await page.request.get('/search?q=qqqzzzxxxvvv');
				const body = (await res.json()) as { items: unknown[] };
				return body.items.length;
			},
			{ timeout: 120_000 }
		)
		.toBeGreaterThan(0);
	await searchBox.fill('qqqzzzxxxvvv');
	await expect(results.getByRole('link').first()).toBeVisible();
	await expect(page.getByText('見つかりませんでした')).not.toBeVisible();

	// 空クエリで active が下り、通常一覧(1ページ目 20件)が戻る
	await searchBox.fill('');
	await expect(page.getByRole('link', { name: /article/ })).toHaveCount(20);
});
