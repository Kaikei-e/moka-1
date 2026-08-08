// 既読マーク(POST /api/v1/articles/{id}/read)とフィード削除(DELETE /api/v1/feeds/{id})
// 移行元: hurl/core/read_and_feed_delete.hurl
//
// 前提: フレッシュ DB。health gate は tests/setup/core-health.setup.ts(setup プロジェクト)に
// 集約済みなのでここでは打たない。実行順序は 01-feeds-and-articles(25記事)/ 02-summarize /
// 05-articles-null-published-at(3記事)の後 — 05 の直後に走ること(README.md の実行順序表を参照)。
// 一覧の先頭2件が urn:moka-e2e-null:1(fallback で全記事中最新) / urn:moka-e2e:3 である前提を使う。
// 末尾で null-pubdate フィードを削除するため、このファイルは「厳密なカウント」に依存する連番
// グループの最後に置いてある(後続の 08-dedupe-no-304 / 10-scheduler は独自フィードだけを使い、
// 07-auth はフィードに触れないので影響しない)。旧 Hurl の `--jobs 1`(DB 依存シナリオなので直列)は
// playwright.config.ts の fullyParallel: false + workers: 1 が受け持つ。
//
// 変数: 旧 Hurl の `--variable host=...` は playwright.config.ts の use.baseURL(support/env.ts の
// coreBaseURL)へ、`--variable fixture_url=...` / `--variable null_pubdate_fixture_url=...` は下の
// fixtureURL(fixtures.main) / fixtureURL(fixtures.nullPubDate) へ移した。
// nullArticleId(null-pubdate フィードの先頭記事 id)/ readArticleId(既読マーク対象の記事 id)/
// delFeedId(削除対象フィード id)は、記事一覧の捕捉からフィード削除の確認まで複数の test() を
// またいで参照するので、モジュールスコープの変数に置く(旧 Hurl の [Captures] 相当)。
// test.describe.configure({ mode: 'serial' }) によりこのファイル内は直列実行(1つ落ちたら
// 後続は skip)が保証されるので、モジュールスコープでの受け渡しが安全に成立する。
import { test, expect } from '@playwright/test';
import { fixtureURL, fixtures } from '../../support/env';
import { expectErrorResponse, json, listArticles, listFeeds } from '../../support/moka-api';
import type { ArticleResponse } from '../../support/types';

test.describe.configure({ mode: 'serial' });

const fixtureUrl = fixtureURL(fixtures.main);
const nullPubDateFixtureUrl = fixtureURL(fixtures.nullPubDate);

let nullArticleId: number;
let readArticleId: number;
let delFeedId: number;

// 2. 一覧の先頭2件: null フィードの item1(fallback で全記事中最新)と本体フィクスチャの
// 最新記事。未読 = read が false(article_reads に行が無い)、feed_title は所属フィードの title
test('2. 一覧の先頭2件は未読で、feed_title が所属フィードの title であること', async ({
	request
}) => {
	const body = await listArticles(request, { limit: 2 });
	const first = body.articles[0];
	const second = body.articles[1];
	expect(first, 'articles[0] が存在すること').toBeDefined();
	expect(second, 'articles[1] が存在すること').toBeDefined();
	nullArticleId = first!.id;
	readArticleId = second!.id;
	expect(first!.guid, 'articles[0].guid').toBe('urn:moka-e2e-null:1');
	expect(first!.read, 'articles[0].read が false であること').toBe(false);
	expect(first!.feed_title, 'articles[0].feed_title').toBe('Moka E2E Null PubDate Fixture');
	expect(second!.guid, 'articles[1].guid').toBe('urn:moka-e2e:3');
	expect(second!.read, 'articles[1].read が false であること').toBe(false);
	expect(second!.feed_title, 'articles[1].feed_title').toBe('Moka E2E Fixture');
});

test('既読マークは冪等(2回目も article_reads に行は増えない)', async ({ request }) => {
	// 3. 既読マーク — 204(ボディ無し)
	await test.step('3. 既読マーク — 204(ボディ無し)', async () => {
		const response = await request.post(`/api/v1/articles/${readArticleId}/read`);
		expect(response.status(), await response.text()).toBe(204);
	});

	// 4. 冪等な再マーク — 204 のまま(article_reads に行は増えない)
	await test.step('4. 冪等な再マーク — 204 のまま(article_reads に行は増えない)', async () => {
		const response = await request.post(`/api/v1/articles/${readArticleId}/read`);
		expect(response.status(), await response.text()).toBe(204);
	});
});

// 5. 一覧に read フラグが現れる。マークしていない記事は false のまま
test('5. 一覧に read フラグが現れる。マークしていない記事は false のまま', async ({ request }) => {
	const body = await listArticles(request, { limit: 2 });
	const first = body.articles[0];
	const second = body.articles[1];
	expect(first, 'articles[0] が存在すること').toBeDefined();
	expect(second, 'articles[1] が存在すること').toBeDefined();
	expect(first!.read, 'articles[0].read が false のままであること').toBe(false);
	expect(second!.read, 'articles[1].read が true であること').toBe(true);
});

// 6. 記事単体(読書ビュー)でも read / feed_title が返る
test('6. 記事単体(読書ビュー)でも read / feed_title が返る', async ({ request }) => {
	const response = await request.get(`/api/v1/articles/${readArticleId}`);
	expect(response.status(), await response.text()).toBe(200);
	const body = await json<ArticleResponse>(response);
	expect(body.article.read, 'article.read が true であること').toBe(true);
	expect(body.article.feed_title, 'article.feed_title').toBe('Moka E2E Fixture');
});

// 7. 存在しない記事の既読マークは 404
test('7. 存在しない記事の既読マークは 404', async ({ request }) => {
	const response = await request.post('/api/v1/articles/999999/read');
	await expectErrorResponse(response, 404);
});

// 8. 数値でない id は 400
test('8. 数値でない記事 id の既読マークは 400', async ({ request }) => {
	const response = await request.post('/api/v1/articles/not-a-number/read');
	await expectErrorResponse(response, 400);
});

// 9. フィード一覧の先頭 = 最後に登録した null-pubdate フィード(created_at DESC, id DESC)
test('9. フィード一覧の先頭は最後に登録した null-pubdate フィードであること', async ({
	request
}) => {
	const body = await listFeeds(request);
	expect(body.feeds, 'feeds が2件であること').toHaveLength(2);
	const first = body.feeds[0];
	expect(first, 'feeds[0] が存在すること').toBeDefined();
	delFeedId = first!.id;
	expect(first!.url, 'feeds[0].url が null-pubdate フィクスチャ URL であること').toBe(
		nullPubDateFixtureUrl
	);
});

test('フィード削除は204、削除済みへの再削除は404', async ({ request }) => {
	// 10. フィード削除 — 204。配下の記事・既読・要約は FK の ON DELETE CASCADE が一括削除する
	await test.step('10. フィード削除 — 204。配下の記事・既読・要約は FK の ON DELETE CASCADE が一括削除する', async () => {
		const response = await request.delete(`/api/v1/feeds/${delFeedId}`);
		expect(response.status(), await response.text()).toBe(204);
	});

	// 11. 二度目は 404(既に無い)
	await test.step('11. 二度目は 404(既に無い)', async () => {
		const response = await request.delete(`/api/v1/feeds/${delFeedId}`);
		await expectErrorResponse(response, 404);
	});
});

// 12. 数値でない id は 400
test('12. 数値でないフィード id の削除は 400', async ({ request }) => {
	const response = await request.delete('/api/v1/feeds/not-a-number');
	await expectErrorResponse(response, 400);
});

// 13. フィード一覧から消えている
test('13. 削除したフィードがフィード一覧から消えている', async ({ request }) => {
	const body = await listFeeds(request);
	expect(body.feeds, 'feeds が1件であること').toHaveLength(1);
	const first = body.feeds[0];
	expect(first, 'feeds[0] が存在すること').toBeDefined();
	expect(first!.url, 'feeds[0].url が本体フィクスチャ URL であること').toBe(fixtureUrl);
});

// 14. 配下の記事も CASCADE で消えている
test('14. 削除したフィード配下の記事も CASCADE で消えている', async ({ request }) => {
	const response = await request.get(`/api/v1/articles/${nullArticleId}`);
	await expectErrorResponse(response, 404);
});

// 15. 一覧は本体フィクスチャの25件に戻り、先頭は既読マークした記事のまま
// (別フィードの削除は他フィードの既読の事実を巻き込まない)
test('15. 一覧は本体フィクスチャの25件に戻り、既読の事実は他フィードの削除に巻き込まれない', async ({
	request
}) => {
	const body = await listArticles(request, { limit: 30 });
	expect(body.articles, 'articles が25件であること').toHaveLength(25);
	const first = body.articles[0];
	expect(first, 'articles[0] が存在すること').toBeDefined();
	expect(first!.guid, 'articles[0].guid').toBe('urn:moka-e2e:3');
	expect(first!.read, 'articles[0].read が true のままであること').toBe(true);
	expect(body.next_cursor, 'next_cursor が無い(終端)こと').toBeNull();
});
