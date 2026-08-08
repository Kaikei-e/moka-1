// 問い返し Q&A(M2: POST /api/v1/articles/{id}/qa — SSE: sources → delta → done)
//
// CI では e2e-llm-mock(決定的な OpenAI 互換モック)がストリーミング補完を返す。
// moka-core → ハイブリッド検索(文脈選定)→ LLM クライアント → SSE → DB 保存という配線は
// 実コードのまま検証し、回答品質はアサートしない(eval/ の管轄)。
// llm 停止時の error イベント(フェイルソフト)は 12-rag-failsoft.spec.ts 側で検証する。
//
// 前提: フレッシュ DB。health gate は tests/setup/core-health.setup.ts(setup プロジェクト)に
// 集約済みなのでここでは打たない。03-search.spec.ts の後・05-articles-null-published-at.spec.ts
// の前に置く(「limit=1 で先頭記事」の前提を崩す独自フィード登録シナリオより先に走らせる。
// e2e/README.md の実行順序表を参照)。
//
// 変数: articleId はモジュールスコープに保持し、test.describe.configure({ mode: 'serial' }) の下で
// 後続 test() へ引き継ぐ(旧 qa.hurl の [Captures] 相当)。
import { test, expect } from '@playwright/test';
import { fixtureURL, fixtures } from '../../support/env';
import { registerFeedOk, listArticles, expectErrorResponse, postSse } from '../../support/moka-api';
import { sseEventNames, sseData } from '../../support/sse';
import { expectInteger } from '../../support/assertions';

test.describe.configure({ mode: 'serial' });

const fixtureUrl = fixtureURL(fixtures.main);

/** done イベントの data(core/internal/httpapi/qa.go の writeEvent("done", ...) と 1:1)。 */
type QaDoneEvent = { question_id: number; answer_id: number };

let articleId: number;

test('2-3. フィクスチャフィードを登録して質問対象の記事 id を取る', async ({ request }) => {
	await test.step('2. フィクスチャフィードを登録(冪等 — 登録件数の検証は 01-feeds-and-articles.spec.ts 側)', async () => {
		await registerFeedOk(request, fixtureUrl);
	});

	await test.step('3. 質問対象の記事 id を取る', async () => {
		const { articles } = await listArticles(request, { limit: 1 });
		const first = articles[0];
		expect(first, '記事が1件以上あること').toBeDefined();
		articleId = first!.id;
	});
});

test('4. 正常系 — SSE のイベント順: sources(文脈記事、回答生成の前)→ delta(回答断片)→ done(question_id / answer_id)。error は出ない', async ({
	request
}) => {
	// 文脈検索はテキスト側単独に縮退しても回答は成立する(検索は増強であって回答の前提条件ではない)
	const response = await postSse(request, `/api/v1/articles/${articleId}/qa`, {
		question: 'この記事の要点を教えてください'
	});
	expect(response.status(), await response.text()).toBe(200);
	expect(response.headers()['content-type'], 'Content-Type が text/event-stream であること').toBe(
		'text/event-stream'
	);

	const body = await response.text();

	// sources が delta より先という順序まで表明する(旧 Hurl は body contains の部分文字列一致
	// でしか書けなかった箇所 — ADR00024 が明示する移行による正しい強化)
	const eventNames = sseEventNames(body);
	const sourcesIdx = eventNames.indexOf('sources');
	const firstDeltaIdx = eventNames.indexOf('delta');
	const doneIdx = eventNames.indexOf('done');
	expect(sourcesIdx, 'event: sources が含まれること').toBeGreaterThanOrEqual(0);
	expect(firstDeltaIdx, 'event: delta が含まれること').toBeGreaterThanOrEqual(0);
	expect(doneIdx, 'event: done が含まれること').toBeGreaterThanOrEqual(0);
	expect(
		sourcesIdx,
		'sources が delta より先に来ること(文脈記事は回答生成の前に届く)'
	).toBeLessThan(firstDeltaIdx);
	expect(firstDeltaIdx, 'delta が done より先に来ること').toBeLessThan(doneIdx);
	expect(eventNames, 'event: error を含まないこと').not.toContain('error');

	expect(body, 'body が question_id を含むこと').toContain('question_id');
	expect(body, 'body が answer_id を含むこと').toContain('answer_id');
	const done = sseData<QaDoneEvent>(body, 'done');
	expect(done, 'done イベントの data が JSON としてパースできること').toBeDefined();
	expectInteger(done!.question_id, 'done.question_id');
	expectInteger(done!.answer_id, 'done.answer_id');
});

test('5. question 欠落は 400(SSE ヘッダ送出前の通常 JSON エラー)', async ({ request }) => {
	const response = await postSse(request, `/api/v1/articles/${articleId}/qa`, {});
	await expectErrorResponse(response, 400);
});

test('5b. 空白のみの question も 400', async ({ request }) => {
	const response = await postSse(request, `/api/v1/articles/${articleId}/qa`, {
		question: '   '
	});
	await expectErrorResponse(response, 400);
});

test('6. 存在しない記事は 404', async ({ request }) => {
	const response = await postSse(request, '/api/v1/articles/999999/qa', {
		question: 'この記事の要点を教えてください'
	});
	await expectErrorResponse(response, 404);
});

test('7. 数値でない id は 400', async ({ request }) => {
	const response = await postSse(request, '/api/v1/articles/not-a-number/qa', {
		question: 'この記事の要点を教えてください'
	});
	await expectErrorResponse(response, 400);
});
