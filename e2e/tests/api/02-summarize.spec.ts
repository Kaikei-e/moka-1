// 記事要約(M1: POST /summary, /summary/stream)
//
// 本物の llm は GPU(iGPU Vulkan passthrough)が要り GitHub-hosted runner では起動できないため、
// CI では compose.e2e.yaml の e2e-llm-mock(決定的な OpenAI 互換モック、e2e/mock-llm/mock_llm.py)
// に対して実行する。moka-core → LLM クライアント → DB保存 → API という配線は実コードのまま検証し、
// 実モデルの応答品質だけがモックに置き換わる(推論品質自体は eval/ の管轄)。
//
// 前提: フレッシュ DB。health gate は tests/setup/core-health.setup.ts(setup プロジェクト)に
// 集約済みなのでここでは打たない。実行順序は 01-feeds-and-articles.spec.ts の直後、
// 03-search.spec.ts 等「順序を崩す登録」より前(README.md の実行順序表)。
// GET /api/v1/articles?limit=1 で先頭記事を掴む前提のため、独自フィードをまだ登録していない
// このタイミングで走らせる。
//
// 変数: articleId / summaryText / firstCreatedAt はモジュールスコープに保持し、
// test.describe.configure({ mode: 'serial' }) の下で後続 test() へ引き継ぐ
// (旧 summarize.hurl の [Captures] 相当)。
//
// 旧 feeds_and_articles.hurl とは別ファイル = 別スコープの変数だったため、summarize.hurl は
// フィード登録から自前で行っていた。Playwright でも同じフィクスチャ(fixtures.main)を
// 冪等に登録し直す(01 が既に登録済みでも安全 — 登録件数の検証は 01-feeds-and-articles.spec.ts 側)。
import { test, expect } from '@playwright/test';
import { fixtureURL, fixtures } from '../../support/env';
import {
	registerFeedOk,
	listArticles,
	expectErrorResponse,
	postSse,
	json
} from '../../support/moka-api';
import type { SummaryResponse } from '../../support/types';
import { sseEventNames, sseDeltaText } from '../../support/sse';
import { expectIsoDate, expectNonEmptyString } from '../../support/assertions';

test.describe.configure({ mode: 'serial' });

let articleId: number;
let summaryText: string;
let firstCreatedAt: string;

test('2-3. フィクスチャフィードを登録して要約対象の記事 id を取る', async ({ request }) => {
	await test.step(
		'2. フィクスチャフィードを登録(同じ実行内で 01-feeds-and-articles.spec.ts が先に登録済みでも' +
			'冪等に安全。201/200 のどちらでもよい — 登録件数の検証は 01 側)',
		async () => {
			await registerFeedOk(request, fixtureURL(fixtures.main));
		}
	);

	await test.step('3. 要約対象の記事 id を取る', async () => {
		const { articles } = await listArticles(request, { limit: 1 });
		const first = articles[0];
		expect(first, '記事が1件以上あること').toBeDefined();
		articleId = first!.id;
	});
});

test('4. 要約 — 初回は新規 201。llm へ実際に投げて要約を得る', async ({ request }) => {
	const response = await request.post(`/api/v1/articles/${articleId}/summary`);
	expect(response.status(), await response.text()).toBe(201);
	const body = await json<SummaryResponse>(response);
	expect(body.summary.article_id, 'summary.article_id が対象記事と一致すること').toBe(articleId);
	expectNonEmptyString(body.summary.text, 'summary.text');
	expectNonEmptyString(body.summary.model_meta.model, 'summary.model_meta.model');
	expectIsoDate(body.summary.created_at, 'summary.created_at');
	summaryText = body.summary.text;
});

test('5. 冪等な再要約 — 200、llm を再度叩かず保存済みの要約をそのまま返す', async ({ request }) => {
	const response = await request.post(`/api/v1/articles/${articleId}/summary`);
	expect(response.status(), await response.text()).toBe(200);
	const body = await json<SummaryResponse>(response);
	expect(body.summary.text, 'キャプチャ済みの summary_text と一致すること').toBe(summaryText);
	firstCreatedAt = body.summary.created_at;
});

test('5b. ?force=true — 既存要約があっても無視して常に新規生成する', async ({ request }) => {
	// 読者が品質に満足できず明示的に「やり直す」場合。article_summaries は INSERT-only なので
	// 新しい行が追記される(ADR00002)。201・model_meta.regenerated == true・古い行とは違う
	// created_at がその証拠(mock-llm はテキスト自体は固定文言を返すため、行が本当に増えたことは
	// 201 と created_at の変化で確認する — feeds_and_articles.hurl の冪等確認と対になるシナリオ)
	const response = await request.post(`/api/v1/articles/${articleId}/summary?force=true`);
	expect(response.status(), await response.text()).toBe(201);
	const body = await json<SummaryResponse>(response);
	expect(body.summary.article_id, 'summary.article_id が対象記事と一致すること').toBe(articleId);
	expect(body.summary.model_meta.regenerated, 'model_meta.regenerated が true であること').toBe(
		true
	);
	expect(body.summary.created_at, '古い行とは違う created_at であること').not.toBe(firstCreatedAt);
});

test('5c. force 無しの通常 POST に戻す — 200 のまま、かつ latest が更新されている', async ({
	request
}) => {
	// 5b で追記された新しい行が返る = ORDER BY created_at DESC LIMIT 1 が効いている
	const response = await request.post(`/api/v1/articles/${articleId}/summary`);
	expect(response.status(), await response.text()).toBe(200);
	const body = await json<SummaryResponse>(response);
	expect(body.summary.model_meta.regenerated, 'model_meta.regenerated が true であること').toBe(
		true
	);
});

test('6. 存在しない記事は 404', async ({ request }) => {
	const response = await request.post('/api/v1/articles/999999/summary');
	await expectErrorResponse(response, 404);
});

test('7. 数値でない id は 400', async ({ request }) => {
	const response = await request.post('/api/v1/articles/not-a-number/summary');
	await expectErrorResponse(response, 400);
});

test('8. ストリーミング要約(専用エンドポイント)', async ({ request }) => {
	// 直前のステップで既に要約済みなので新規生成せず(llm を再度叩かない)、
	// SSE で1回の delta に全文を乗せてから done を返す
	const response = await postSse(
		request,
		`/api/v1/articles/${articleId}/summary/stream`,
		undefined
	);
	expect(response.status(), await response.text()).toBe(200);
	expect(response.headers()['content-type'], 'Content-Type が text/event-stream であること').toBe(
		'text/event-stream'
	);

	const body = await response.text();
	const eventNames = sseEventNames(body);
	expect(eventNames, '1回の delta の後に done が来ること(順序込み)').toEqual(['delta', 'done']);
	expect(eventNames, 'event: error を含まないこと').not.toContain('error');

	const deltaText = sseDeltaText(body);
	expect(deltaText, 'delta 連結テキストに要約本文(summary_text)が含まれること').toContain(
		summaryText
	);
});

test('9. 存在しない記事は 404(SSE ヘッダ送出前の通常 JSON エラー)', async ({ request }) => {
	const response = await postSse(request, '/api/v1/articles/999999/summary/stream', undefined);
	await expectErrorResponse(response, 404);
});

test('10. 数値でない id は 400', async ({ request }) => {
	const response = await postSse(
		request,
		'/api/v1/articles/not-a-number/summary/stream',
		undefined
	);
	await expectErrorResponse(response, 400);
});
