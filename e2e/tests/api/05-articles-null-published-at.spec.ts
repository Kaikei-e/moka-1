// published_at が NULL の記事のフォールバック順(COALESCE(published_at, created_at))
// 移行元: hurl/core/articles_null_published_at.hurl
//
// 前提: フレッシュ DB。health gate は tests/setup/core-health.setup.ts(setup プロジェクト)に
// 集約済みなのでここでは打たない。実行順はファイル名の連番がそのまま前提になる: 01-feeds-and-articles
// (25記事)/ 02-summarize / 03-search / 04-qa の後に走ること。NULL 記事が fallback で一覧の先頭に来る
// ため、02〜04 の「limit=1 で先頭記事を掴む」前提を崩す — だからこのファイルはそれらより後ろに置いて
// ある(e2e/README.md の実行順序表を参照)。旧 Hurl の `--jobs 1`(DB 依存シナリオなので直列)は
// playwright.config.ts の fullyParallel: false + workers: 1 が受け持つ。
//
// フィクスチャは3記事: item1 は pubDate 無し(published_at NULL、fallback で「今」扱い
// = 全記事中で最新になるはず)。item2(2026-04-01)/item3(2026-03-01)は feed.xml 側の
// 日付レンジ(2026-06-07〜07-01)より確実に古い過去日付にしてあり、他ファイルの記事と混線しない。
//
// 変数: 旧 Hurl の `--variable host=...` は playwright.config.ts の use.baseURL(support/env.ts の
// coreBaseURL)へ、`--variable null_pubdate_fixture_url=http://e2e-fixtures/feed-null-pubdate.xml`
// (例)は下の fixtureURL(fixtures.nullPubDate) へ移した。
// cursor1 / cursor2 は3ページを繋ぐカーソル(旧 Hurl の [Captures] cursor_1 / cursor_2 相当)。
// 3ページとも同一シナリオの中でしか使わないので、1つの test() の中で test.step ごとに
// 取り直しながら使い切るローカル変数で足りる(モジュールスコープへ昇格させる必要はない)。
import { test, expect } from '@playwright/test';
import { fixtureURL, fixtures } from '../../support/env';
import { registerFeed, listArticles, json } from '../../support/moka-api';
import type { FeedRegistration } from '../../support/types';

test.describe.configure({ mode: 'serial' });

const nullPubDateFixtureUrl = fixtureURL(fixtures.nullPubDate);

// 2. フィクスチャフィードを登録
test('2. published_at NULL を含むフィクスチャフィードを登録する', async ({ request }) => {
	const response = await registerFeed(request, nullPubDateFixtureUrl);
	expect(response.status(), await response.text()).toBe(201);
	const body = await json<FeedRegistration>(response);
	expect(body.inserted_articles, 'inserted_articles が3件であること').toBe(3);
});

test('記事一覧を3ページ通して読み、NULL published_at のフォールバック順を確認する', async ({
	request
}) => {
	let cursor1: string;
	let cursor2: string;

	// 3. 記事一覧の先頭(limit=1, カーソル無し)— published_at が NULL の記事が
	// 「最下部に沈む」のではなく、fallback(created_at≈登録時刻)で全記事中の最新として
	// 先頭に来ることを確認する(修正前は NULLS LAST で最後尾に落ちるため、ここで失敗する)
	await test.step('3. 記事一覧の先頭(limit=1) — NULL published_at の記事が fallback で全記事中最新として先頭に来ること', async () => {
		const body = await listArticles(request, { limit: 1 });
		const first = body.articles[0];
		expect(first, 'articles[0] が存在すること').toBeDefined();
		expect(first!.guid, 'articles[0].guid').toBe('urn:moka-e2e-null:1');
		expect(first!.published_at, 'articles[0].published_at が null であること').toBeNull();
		expect(typeof body.next_cursor, 'next_cursor が文字列であること').toBe('string');
		cursor1 = body.next_cursor as string;
	});

	// 4. 続く25件(01-feeds-and-articles.spec.ts のフィクスチャ、2026-07-01〜06-07)を丸ごと1ページで
	// 消費する。published_at が NULL だった item1 が重複して混ざり込まないことを確認する
	await test.step('4. 続く25件を丸ごと1ページで消費し、NULL 記事(item1)が重複混入しないこと', async () => {
		const body = await listArticles(request, { limit: 25, cursor: cursor1 });
		expect(body.articles, 'articles が25件であること').toHaveLength(25);
		const first = body.articles[0];
		const last = body.articles[24];
		expect(first, 'articles[0] が存在すること').toBeDefined();
		expect(last, 'articles[24] が存在すること').toBeDefined();
		expect(first!.guid, 'articles[0].guid').toBe('urn:moka-e2e:3');
		expect(last!.guid, 'articles[24].guid').toBe('urn:moka-e2e:25');
		expect(typeof body.next_cursor, 'next_cursor が文字列であること').toBe('string');
		cursor2 = body.next_cursor as string;
	});

	// 5. 残りの2件(item2, item3、日付順)を最後のページで取得。limit を残数(2件)より
	// 大きくして「満杯ページ」を避け、重複なく next_cursor が無い(終端)ことを確認する
	await test.step('5. 残りの2件を最後のページで取得し、重複なく next_cursor が無い(終端)こと', async () => {
		const body = await listArticles(request, { limit: 5, cursor: cursor2 });
		expect(body.articles, 'articles が2件であること').toHaveLength(2);
		const first = body.articles[0];
		const second = body.articles[1];
		expect(first, 'articles[0] が存在すること').toBeDefined();
		expect(second, 'articles[1] が存在すること').toBeDefined();
		expect(first!.guid, 'articles[0].guid').toBe('urn:moka-e2e-null:2');
		expect(second!.guid, 'articles[1].guid').toBe('urn:moka-e2e-null:3');
		expect(body.next_cursor, 'next_cursor が無い(終端)こと').toBeNull();
	});
});
