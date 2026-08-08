// enrich.Scheduler(常駐エージェントループの濃縮ステップ、tenets §3.2 step3)が、人間・
// API起点の要約/タグ生成リクエスト無しに、新着記事へ自動で summary/tags を付与することを
// 検証する。このファイルは POST .../summary / POST .../tags を一度も呼ばない —
// それでも中身が見える(GET のみで確認できる)ことそのものが M1 の Done 条件の主張。
// 移行元: hurl/core/enrich_e2e.sh + hurl/core/enrich_poll.hurl
//
// 前提: フレッシュ DB。health gate は tests/setup/core-health.setup.ts(setup プロジェクト)に
// 集約済みなのでここでは打たない。実行順序は 08-dedupe-no-304.spec.ts の直後(README.md の
// 実行順序表)。独自フィード(fixtures.enrich)を登録するので他の厳密なカウントには影響しない。
//
// 変数: articleId は「guid で記事を特定する」test() から、summary/tags のポーリングを行う
// test() まで複数の test() をまたいで参照するので、モジュールスコープの変数に置く
// (旧 Hurl の enrich_poll.hurl 変数 article_id、旧 bash の article_id 変数に相当)。
import { test, expect } from '@playwright/test';
import { fixtureURL, fixtures } from '../../support/env';
import { registerFeed, findArticleByGuid, json } from '../../support/moka-api';
import { expectIsoDate, expectNonEmptyString } from '../../support/assertions';
import type { SummaryResponse, TagsResponse } from '../../support/types';

test.describe.configure({ mode: 'serial' });

const enrichFixtureUrl = fixtureURL(fixtures.enrich);
const enrichGuid = 'urn:moka-e2e-enrich:1';

let articleId: number;

test('1-2. フィクスチャフィードを登録(同期・API起点)して guid で自分の記事を引く', async ({
	request
}) => {
	// 1. フィードを登録(同期・API起点。ここまではユーザー操作の代替 — 要約/タグへの
	//    リクエストはまだ一切していない)
	await test.step('1. フィードを登録(同期・API起点)', async () => {
		const response = await registerFeed(request, enrichFixtureUrl);
		expect(response.status(), await response.text()).toBe(201);
	});

	// 2. guid で自分の記事を引く(feed-dedupe.xml が pubDate 2030年で一覧の先頭を占有する
	//    ため、「一覧の先頭」には頼れない — 固定 guid での直接検索が唯一の決定的な方法)
	await test.step('2. guid で自分の記事を引く', async () => {
		const article = await findArticleByGuid(request, enrichGuid);
		expect(article, `guid=${enrichGuid} の記事が見つかること`).toBeDefined();
		articleId = article!.id;
	});
});

// 3. enrich.Scheduler が自律的に summary/tags を付けるまでポーリングする
// (人間・API起点の POST は一切無し。POST .../summary / POST .../tags は本ファイルで一度も呼ばない —
// それを呼ばずに GET だけで中身が見えることそのものがこのテストの主張)
test('3. enrich.Scheduler が自律的に summary を付与するまでポーリングする(POST は一切呼ばない)', async ({
	request
}) => {
	await expect(async () => {
		const response = await request.get(`/api/v1/articles/${articleId}/summary`);
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<SummaryResponse>(response);
		expect(body.summary.article_id, 'summary.article_id が対象記事と一致すること').toBe(articleId);
		expectNonEmptyString(body.summary.text, 'summary.text');
		expectNonEmptyString(body.summary.model_meta.model, 'summary.model_meta.model');
		expectIsoDate(body.summary.created_at, 'summary.created_at');
	}).toPass({ intervals: [1000], timeout: 20_000 });
});

// 4. 同様に enrich.Scheduler が自律的に tags を付与するまでポーリングする(POST は一切呼ばない)
test('4. enrich.Scheduler が自律的に tags を付与するまでポーリングする(POST は一切呼ばない)', async ({
	request
}) => {
	await expect(async () => {
		const response = await request.get(`/api/v1/articles/${articleId}/tags`);
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<TagsResponse>(response);
		expect(body.tags.length, 'tags が1件以上であること').toBeGreaterThanOrEqual(1);
	}).toPass({ intervals: [1000], timeout: 20_000 });
});
