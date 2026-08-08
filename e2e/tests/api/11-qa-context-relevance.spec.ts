// 問い返し Q&A の文脈検索クエリが対象記事のタイトルを連結して構成されることを検証する
// (RAG 精度改善 変更1: タイトル連結、移行元: qa_context_relevance_e2e.sh +
// qa_context_relevance.hurl)。独自フィード(feed-qa-context.xml、同一タイトル・別トピックの
// 2記事)を登録するので、他の厳密なカウントアサーションを崩さないよう 09-enrich.spec.ts /
// 10-scheduler.spec.ts の後、12-rag-failsoft.spec.ts(e2e-llm-mock を止める)より前に置く。
//
// トピック語を含まない一般要約質問(04-qa.spec.ts と同じ文言)を対象記事(記事1)へ投げると、
// 対象記事のタイトルが検索クエリへ連結されて初めて、同タイトルを持つもう一方の記事
// (記事2、内容は無関係)が文脈選定のヒット集合に入ってくる — これがクエリ構成の変更
// (質問文のみ→タイトル+質問文)を外部から観測できる唯一の配線レベルの契約。意味的な
// 関連性そのものはモック埋め込みでは判定できない(e2e/README.md の e2e-llm-mock 節参照)。
//
// 前提: フレッシュ DB、e2e-llm-mock が生きていること。health gate は
// tests/setup/core-health.setup.ts(setup プロジェクト)に集約済みなのでここでは打たない。
// ベクトル側(article_embeddings)が対象記事を埋め込み終わるまで enrich.Scheduler の埋め込みを
// 待つ必要がある(feed-qa-context.xml 側にも title 由来の trigram 手がかりがあるが、埋め込みが
// 済んでいるほうがクエリ構成変更の効果がより安定して観測できる)。
//
// 変数: targetArticleId(記事1の id)はモジュールスコープに保持し、
// test.describe.configure({ mode: 'serial' }) の下で後続 test() へ引き継ぐ
// (旧シェルの target_article_id 相当)。distractorMarker(記事2の URL に含まれる一意な
// 文字列 "qactx-2")は qa_context_relevance.hurl の distractor_marker 変数をそのまま定数化。
import { test, expect } from '@playwright/test';
import { fixtureURL, fixtures } from '../../support/env';
import { registerFeedOk, findArticleByGuid, json, postSse } from '../../support/moka-api';
import type { SearchResponse } from '../../support/types';
import { expectEventStream, expectNoSseError, sseEventNames, sseData } from '../../support/sse';

test.describe.configure({ mode: 'serial' });

const fixtureUrl = fixtureURL(fixtures.qaContext);

/** sources イベントの data(core/internal/httpapi/qa.go の writeEvent("sources", ...) と 1:1)。 */
type QaSourcesEvent = { articles: { id: number; title: string; url: string }[] };

/** 記事2の URL(http://e2e-fixtures/articles/qactx-2)に含まれる一意な文字列。 */
const distractorMarker = 'qactx-2';

let targetArticleId: number;

test('1-2. フィクスチャフィードを登録して対象記事(記事1)の id を取る', async ({ request }) => {
	await test.step('1. フィードを登録(同期・API起点)', async () => {
		await registerFeedOk(request, fixtureUrl);
	});

	await test.step('2. guid で対象記事(記事1)の id を引く(一覧の先頭には頼れない — enrich_e2e.sh と同じ理由)', async () => {
		const article = await findArticleByGuid(request, 'urn:moka-e2e-qactx:1', 200);
		expect(article, 'guid=urn:moka-e2e-qactx:1 の記事が見つかること').toBeDefined();
		targetArticleId = article!.id;
	});
});

test('3. ベクトル側(article_embeddings)が対象記事を埋め込み終わるまで待つ', async ({ request }) => {
	// テキスト側(pg_trgm)だけでも観測できるはずのシグナルだが、埋め込みが効いた通常状態で
	// 検証する。元は `until curl -sf .../search?q=Zyloforge%20Marmalade | jq -e '.items|length>=1'`
	// を最大30回・2秒間隔でポーリング(約60秒でタイムアウト)。
	//
	// expect.poll ではなく toPass を使う: expect.poll は「取得関数そのものが投げた例外」を
	// 再試行せずそのまま送出する(matcher の失敗しか再試行しない)。旧 `until curl -sf` は
	// HTTP エラーでも再試行していたので、リクエスト/ステータス検査ごと再試行する toPass が
	// 忠実な対応になる。config の既定テスト timeout(120秒)が 60秒のポーリングを覆う
	await expect(async () => {
		const response = await request.get('/api/v1/search', {
			params: { q: 'Zyloforge Marmalade' }
		});
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<SearchResponse>(response);
		expect(
			body.items.length,
			'対象記事の埋め込みが済み、タイトル語のクエリが1件以上ヒットすること'
		).toBeGreaterThanOrEqual(1);
	}).toPass({ intervals: [2000], timeout: 60_000 });
});

test('4. 質問(トピック語なし)へのクエリ配線を検証する — distractor_marker(記事2)が sources に現れること', async ({
	request
}) => {
	// 質問文単独では記事1・記事2どちらの英字タイトルとも pg_trgm 類似度が付かない(質問は
	// 日本語、フィクスチャ群は英字タイトルのみ)ため、この観測は「クエリにタイトルが乗って
	// いるかどうか」だけに依存する
	const response = await postSse(request, `/api/v1/articles/${targetArticleId}/qa`, {
		question: 'この記事の要点を教えてください'
	});
	expect(response.status(), await response.text()).toBe(200);
	expectEventStream(response);

	const body = await response.text();
	const eventNames = sseEventNames(body);
	expect(eventNames, 'event: sources が含まれること').toContain('sources');
	expect(eventNames, 'event: done が含まれること').toContain('done');
	// 旧 Hurl の `body not contains "event: error"`
	expectNoSseError(body);
	expect(
		eventNames.indexOf('sources'),
		'sources が done より先に来ること(文脈記事は回答生成の前に届く)'
	).toBeLessThan(eventNames.indexOf('done'));

	expect(body, `body が distractor_marker(${distractorMarker})を含むこと`).toContain(
		distractorMarker
	);

	// distractor_marker が sources イベントの中に現れることまで確かめる(旧 Hurl の
	// body contains は body 全体への部分文字列一致でしか書けなかった箇所 — 移行による強化)
	const sources = sseData<QaSourcesEvent>(body, 'sources');
	expect(sources, 'sources イベントの data が JSON としてパースできること').toBeDefined();
	const sourceUrls = sources!.articles.map((a) => a.url);
	expect(
		sourceUrls.some((url) => url.includes(distractorMarker)),
		`distractor_marker(${distractorMarker})を含む記事(記事2)が sources に現れること` +
			'(対象記事のタイトルが検索クエリへ連結された証拠)'
	).toBe(true);
});
