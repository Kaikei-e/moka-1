// ハイブリッド検索(M2: GET /api/v1/search — pg_trgm + pgvector の RRF 融合、ADR00022)
//
// e2e-llm-mock は /embeddings(決定的な 3-gram feature hashing、1024次元)も実装しており、
// enrich.Scheduler が記事を自動で埋め込む。ここではテキスト側(pg_trgm)の契約に加えて、
// ベクトル側(pgvector 近傍)が RRF に効くケースも検証する(#6 — 埋め込みの完了を retry で待つ)。
// llm 停止時のテキスト単独縮退 + ヒットなし空配列 + Q&A error は 12-rag-failsoft.spec.ts 側で検証する。
//
// 前提: フレッシュ DB。health gate は tests/setup/core-health.setup.ts(setup プロジェクト)に
// 集約済みなのでここでは打たない。02-summarize.spec.ts の後・05-articles-null-published-at.spec.ts
// の前に置く(独自フィードを追加する後続シナリオが検索ヒットに混ざらないうちに走らせる。
// e2e/README.md の実行順序表を参照)。
//
// 変数: fixtureUrl はモジュールスコープの定数。test.describe.configure({ mode: 'serial' }) の下、
// 各 test() は独立したクエリを打つだけで手順が線形に依存しないため、旧 Hurl の [Captures] に
// 相当するテストをまたぐ捕捉変数は無い(01-feeds-and-articles.spec.ts / 02-summarize.spec.ts の
// articleId と違い、ここではフィード登録以外に前提となる状態は無い)。
import { test, expect } from '@playwright/test';
import type { APIRequestContext, APIResponse } from '@playwright/test';
import { fixtureURL, fixtures } from '../../support/env';
import { registerFeedOk, expectErrorResponse, json } from '../../support/moka-api';
import type { SearchResponse } from '../../support/types';
import { expectIsoDate, expectInteger, expectFloat } from '../../support/assertions';

test.describe.configure({ mode: 'serial' });

const fixtureUrl = fixtureURL(fixtures.main);

/** GET /api/v1/search — クエリパラメータを渡すだけの薄いラッパ(このファイル専用のローカルヘルパ)。 */
async function search(
	request: APIRequestContext,
	params: Record<string, string | number>
): Promise<APIResponse> {
	return request.get('/api/v1/search', { params });
}

test('2. フィクスチャフィードを登録(冪等 — 同じ実行内で登録済みなら 200 / 挿入 0)', async ({
	request
}) => {
	await registerFeedOk(request, fixtureUrl);
});

test('3. 空クエリは 400', async ({ request }) => {
	const response = await request.get('/api/v1/search');
	await expectErrorResponse(response, 400);
});

test('3b. 空白のみのクエリも 400(TrimSpace 後に空)', async ({ request }) => {
	const response = await search(request, { q: '  ' });
	await expectErrorResponse(response, 400);
});

test('4. 不正な limit は 400(limit=0)', async ({ request }) => {
	const response = await search(request, { q: 'article', limit: 0 });
	await expectErrorResponse(response, 400);
});

test('4. 不正な limit は 400(limit=not-a-number)', async ({ request }) => {
	const response = await search(request, { q: 'article', limit: 'not-a-number' });
	await expectErrorResponse(response, 400);
});

test('5. ヒットあり — タイトル完全一致が先頭。封筒は items、記事表現は一覧 API と同じ', async ({
	request
}) => {
	// (feed.Article 埋め込み)+ RRF 融合スコア(score)。埋め込みの有無(enrich.Scheduler の
	// 進み具合)に依らずテキスト側 rank 1 の「Third article」が先頭に来る(ベクトル側でも
	// 3-gram 重なり最大 = 同記事が上位に来るため、RRF 融合後も順位は崩れない)。
	// 埋め込みが部分的にしか済んでいない一瞬(Third 未埋め込みで他記事だけベクトル側に居る)は
	// RRF の同点タイブレークで順位が入れ替わりうるため retry で乗り切る
	await expect(async () => {
		const response = await search(request, { q: 'Third article' });
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<SearchResponse>(response);
		expect(body.items.length, 'items が1件以上であること').toBeGreaterThanOrEqual(1);
		const first = body.items[0];
		expect(first, 'items[0] が存在すること').toBeDefined();
		expect(first!.title, 'items[0].title が "Third article" であること').toBe('Third article');
		expect(first!.guid, 'items[0].guid が "urn:moka-e2e:3" であること').toBe('urn:moka-e2e:3');
		expectInteger(first!.id, 'items[0].id');
		expectIsoDate(first!.published_at, 'items[0].published_at');
		expectFloat(first!.score, 'items[0].score');
	}).toPass({ intervals: [2000], timeout: 60_000 });
});

test('5b. limit が効く(上限まで絞られる)', async ({ request }) => {
	// retry は #5 と同じ理由
	await expect(async () => {
		const response = await search(request, { q: 'Third article', limit: 1 });
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<SearchResponse>(response);
		expect(body.items, 'items がちょうど1件であること').toHaveLength(1);
		const first = body.items[0];
		expect(first, 'items[0] が存在すること').toBeDefined();
		expect(first!.title, 'items[0].title が "Third article" であること').toBe('Third article');
	}).toPass({ intervals: [2000], timeout: 60_000 });
});

test('6. ベクトル側が効く — テキスト側(pg_trgm)が1件も返さない無意味クエリでも近傍ヒットが返る', async ({
	request
}) => {
	// 埋め込み済み記事が cosine 近傍として返る = ヒットは純粋にベクトル側(article_embeddings)由来。
	// enrich.Scheduler の自動埋め込み(MOKA_ENRICH_TICK_SECONDS=2)が最初の記事を埋め込み終わるまで
	// retry で待つ。
	// 注: この挙動により「ヒットなし = 空配列」は埋め込みが存在する限り観測できない
	// (top-k 近傍検索に閾値は無い)。空配列の契約は llm 停止時(ベクトル側が縮退で空)の
	// 12-rag-failsoft.spec.ts 側で検証する
	test.setTimeout(150_000);
	await expect(async () => {
		const response = await search(request, { q: 'qqqzzzxxxvvv' });
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<SearchResponse>(response);
		expect(body.items.length, 'items が1件以上であること').toBeGreaterThanOrEqual(1);
		const first = body.items[0];
		expect(first, 'items[0] が存在すること').toBeDefined();
		expectInteger(first!.id, 'items[0].id');
		expectFloat(first!.score, 'items[0].score');
	}).toPass({ intervals: [2000], timeout: 120_000 });
});

test('6b. ベクトル側が生きた後も、テキスト×ベクトルの RRF 融合で「Third article」が先頭のまま', async ({
	request
}) => {
	// #6 は「最初の1記事」の埋め込みしか待っていない(batch 20/tick なので全25記事は複数 tick
	// かかる)。Third 未埋め込みの窓では #5 と同じ同点タイブレークで別記事が先頭に来うるため、
	// ここも retry で全記事の埋め込み完了まで乗り切る
	await expect(async () => {
		const response = await search(request, { q: 'Third article' });
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<SearchResponse>(response);
		expect(body.items.length, 'items が1件以上であること').toBeGreaterThanOrEqual(1);
		const first = body.items[0];
		expect(first, 'items[0] が存在すること').toBeDefined();
		expect(first!.title, 'items[0].title が "Third article" であること').toBe('Third article');
	}).toPass({ intervals: [2000], timeout: 60_000 });
});
