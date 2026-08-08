import { test, expect } from '@playwright/test';
import { fixtureURL, fixtures } from '../../support/env';
import {
	expectErrorResponse,
	json,
	listArticles,
	listFeeds,
	registerFeed
} from '../../support/moka-api';
import { expectIsoDate } from '../../support/assertions';
import type { ArticleResponse, FeedRegistration, FullTextResponse } from '../../support/types';

// RSS フィード登録 → 記事一覧(M0: feed registered → articles readable)
// 移行元: hurl/core/feeds_and_articles.hurl
//
// 前提: フレッシュ DB(e2e/README.md の「フレッシュ状態にする」手順で e2e-db をリセットした状態)。
// 旧 Hurl の `--jobs 1`(DB 依存シナリオなので直列)は playwright.config.ts の
// fullyParallel: false + workers: 1 が受け持つ。
// moka-core の起動待ち(旧 Hurl 冒頭の retry: 10 / retry-interval: 2000 付き GET /healthz)は
// tests/setup/core-health.setup.ts の setup プロジェクトに集約済みなので、このファイルでは書かない。
// このファイルは実行順で最初の API spec(01-)であることが前提: 「フィード1件・記事ちょうど25件」
// という厳密なカウントのアサーションを含むため、他のフィードをまだ登録していない状態で走る
// 必要がある(e2e/README.md の実行順表を参照)。
//
// 変数: 旧 Hurl の `--variable host=http://localhost:8080`(例)は playwright.config.ts の
// use.baseURL(support/env.ts の coreBaseURL)へ、`--variable fixture_url=http://e2e-fixtures/feed.xml`
// (例)は下の fixtureURL(fixtures.main) へ移した。
// キャプチャ相当の変数のうち feedId(フィード登録直後の冪等性確認だけで使う)・nextCursor・
// fulltextText は、捕捉した test() の中だけで使い切るのでローカル変数で足りる。articleId は
// 複数の test() をまたいで参照する(記事単体取得・全文取り寄せの2箇所)ので、モジュール
// スコープの変数に置く。test.describe.configure({ mode: 'serial' }) によりこのファイル内は
// 直列実行(1つ落ちたら後続は skip)が保証されるので、モジュールスコープでの受け渡しが
// 安全に成立する。
let articleId: number;

const fixtureUrl = fixtureURL(fixtures.main);

test.describe.configure({ mode: 'serial' });

test('新規フィード登録は記事を同期的に取り込み、再登録は冪等に0件を返す', async ({ request }) => {
	let feedId: number;

	// 2. フィクスチャフィードを登録 → 検証・取得・パース・保存まで同期実行
	await test.step('2. フィクスチャフィードを登録 → 検証・取得・パース・保存まで同期実行', async () => {
		const response = await registerFeed(request, fixtureUrl);
		expect(response.status(), await response.text()).toBe(201);
		const body = await json<FeedRegistration>(response);
		feedId = body.feed.id;
		expect(body.feed.url, 'feed.url がフィクスチャ URL と一致すること').toBe(fixtureUrl);
		expect(body.feed.title, 'feed.title が "Moka E2E Fixture" であること').toBe('Moka E2E Fixture');
		expectIsoDate(body.feed.created_at, 'feed.created_at');
		expect(body.inserted_articles, 'inserted_articles が25件であること').toBe(25);
	});

	// 3. 冪等な再登録 → 200、条件付き GET(nginx ネイティブ 304)で挿入 0
	await test.step('3. 冪等な再登録 → 200、条件付き GET(nginx ネイティブ 304)で挿入 0', async () => {
		const response = await registerFeed(request, fixtureUrl);
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<FeedRegistration>(response);
		expect(body.feed.id, 'feed.id が新規登録時から変わらないこと').toBe(feedId);
		expect(body.inserted_articles, '再登録では新規挿入が無いこと').toBe(0);
	});
});

// 4. 不正スキームは 400
test('不正スキームは 400', async ({ request }) => {
	const response = await registerFeed(request, 'ftp://example.com/feed');
	await expectErrorResponse(response, 400);
});

test('記事一覧は新しい順(published_at DESC)のカーソルベースページングで返る', async ({
	request
}) => {
	let nextCursor: string;

	// 5. 記事一覧 — 新しい順(published_at DESC)、カーソルベースページング(offset は持たない)
	await test.step('5. 記事一覧 — 新しい順(published_at DESC)、カーソルベースページング(offset は持たない)', async () => {
		const body = await listArticles(request, { limit: 2 });
		expect(body.articles, 'articles が2件であること').toHaveLength(2);
		const first = body.articles[0];
		const second = body.articles[1];
		expect(first, 'articles[0] が存在すること').toBeDefined();
		expect(second, 'articles[1] が存在すること').toBeDefined();
		articleId = first!.id;
		expect(first!.guid, 'articles[0].guid').toBe('urn:moka-e2e:3');
		expect(first!.title, 'articles[0].title').toBe('Third article');
		expectIsoDate(first!.published_at, 'articles[0].published_at');
		expect(second!.guid, 'articles[1].guid').toBe('urn:moka-e2e:2');
		expect(typeof body.next_cursor, 'next_cursor が文字列であること').toBe('string');
		nextCursor = body.next_cursor as string;
	});

	// 5b. カーソルで次ページ — 重複も取りこぼしも無く、archive 記事(guid 4-25)へ続く
	// (フィクスチャは25件。終端の null 確認は infinite-scroll シナリオ側でカバー)
	await test.step('5b. カーソルで次ページ — 重複も取りこぼしも無く、archive 記事(guid 4-25)へ続く', async () => {
		const body = await listArticles(request, { limit: 2, cursor: nextCursor });
		expect(body.articles, 'articles が2件であること').toHaveLength(2);
		const first = body.articles[0];
		const second = body.articles[1];
		expect(first, 'articles[0] が存在すること').toBeDefined();
		expect(second, 'articles[1] が存在すること').toBeDefined();
		expect(first!.guid, 'articles[0].guid').toBe('urn:moka-e2e:1');
		expect(second!.guid, 'articles[1].guid').toBe('urn:moka-e2e:4');
		expect(typeof body.next_cursor, 'next_cursor が文字列であること').toBe('string');
	});
});

// 5c. 壊れたカーソルは 400
test('壊れたカーソルは 400', async ({ request }) => {
	const response = await request.get('/api/v1/articles', { params: { cursor: 'not-a-cursor' } });
	await expectErrorResponse(response, 400);
});

// 6. 登録済みフィード一覧 — フィード管理画面のデータ源
test('登録済みフィード一覧を取得できる(フィード管理画面のデータ源)', async ({ request }) => {
	const body = await listFeeds(request);
	expect(body.feeds, 'feeds が1件であること').toHaveLength(1);
	const feed = body.feeds[0];
	expect(feed, 'feeds[0] が存在すること').toBeDefined();
	expect(feed!.url, 'feeds[0].url がフィクスチャ URL と一致すること').toBe(fixtureUrl);
	expect(feed!.title, 'feeds[0].title が "Moka E2E Fixture" であること').toBe('Moka E2E Fixture');
	expectIsoDate(feed!.created_at, 'feeds[0].created_at');
});

// 7. 記事単体 — 読書ビューのデータ源
test('記事単体を取得できる(読書ビューのデータ源)', async ({ request }) => {
	const response = await request.get(`/api/v1/articles/${articleId}`);
	expect(response.status(), await response.text()).toBe(200);
	const body = await json<ArticleResponse>(response);
	expect(body.article.id, 'article.id').toBe(articleId);
	expect(body.article.guid, 'article.guid').toBe('urn:moka-e2e:3');
	expect(body.article.title, 'article.title').toBe('Third article');
	expect(body.article.content, 'article.content が存在すること').toBeDefined();
});

// 8. 存在しない記事は 404
test('存在しない記事は 404', async ({ request }) => {
	const response = await request.get('/api/v1/articles/999999');
	await expectErrorResponse(response, 404);
});

// 9. 数値でない id は 400
test('数値でない記事 id は 400', async ({ request }) => {
	const response = await request.get('/api/v1/articles/not-a-number');
	await expectErrorResponse(response, 400);
});

test('全文取り寄せは初回 201、再取り寄せは保存済みの全文をそのまま返す(200)', async ({
	request
}) => {
	let fulltextText: string;

	// 10. 全文取り寄せ — 初回は新規 201。フィクスチャ記事ページから抽出され、
	// RSS 由来の概要(description)より詳しい本文が返る
	await test.step('10. 全文取り寄せ — 初回は新規 201。フィクスチャ記事ページから抽出され、RSS 由来の概要(description)より詳しい本文が返る', async () => {
		const response = await request.post(`/api/v1/articles/${articleId}/fulltext`);
		expect(response.status(), await response.text()).toBe(201);
		const body = await json<FullTextResponse>(response);
		fulltextText = body.fulltext.text;
		expect(body.fulltext.article_id, 'fulltext.article_id').toBe(articleId);
		expect(body.fulltext.text, 'fulltext.text が "ninety-nine" を含むこと').toContain(
			'ninety-nine'
		);
		expectIsoDate(body.fulltext.fetched_at, 'fulltext.fetched_at');
	});

	// 11. 冪等な再取り寄せ — 200、外部サイトを再度叩かず保存済みの全文をそのまま返す
	await test.step('11. 冪等な再取り寄せ — 200、外部サイトを再度叩かず保存済みの全文をそのまま返す', async () => {
		const response = await request.post(`/api/v1/articles/${articleId}/fulltext`);
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<FullTextResponse>(response);
		expect(body.fulltext.text, 'fulltext.text が変わらないこと').toBe(fulltextText);
	});
});

// 12. 存在しない記事は 404
test('存在しない記事の全文取り寄せは 404', async ({ request }) => {
	const response = await request.post('/api/v1/articles/999999/fulltext');
	await expectErrorResponse(response, 404);
});

// 13. 数値でない id は 400
test('数値でない記事 id の全文取り寄せは 400', async ({ request }) => {
	const response = await request.post('/api/v1/articles/not-a-number/fulltext');
	await expectErrorResponse(response, 400);
});

// 要約(POST /summary, /summary/stream)は summarize.hurl に分離した(CI ではモック LLM を使う)
// → 02-summarize.spec.ts へ移行(このファイルの担当外)
