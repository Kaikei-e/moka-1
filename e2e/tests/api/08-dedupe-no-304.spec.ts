// 条件付きGETが効かない(=毎回200で同じ内容を返す)フィードを再取得しても、
// articles.id(IDENTITYシーケンス)の欠番が増えないことを検証する回帰テスト。
// nginx は静的ファイルの mtime から ETag を計算するため、内容を変えずに touch するだけで
// 「サーバが conditional GET を無視して 200 を返す」状況を再現できる
// (通常の再登録シナリオは 01-feeds-and-articles.spec.ts 側で 304 経路を検証済み)。
// 移行元: hurl/core/dedupe_no_304_e2e.sh + hurl/core/dedupe_no_304.hurl
//
// 前提: moka-core / e2e-fixtures / e2e-db が起動済み、フレッシュ DB。
// (フィクスチャの配信実体を書き換えて ETag を動かすシナリオなので、e2e-fixtures が
// e2e/fixtures/ を ro bind mount して配信していることが本質的な前提。)
// health gate は tests/setup/core-health.setup.ts(setup プロジェクト)に集約済みなので
// ここでは打たない。実行順序は 07-auth.spec.ts の直後(README.md の実行順序表)。
// 独自フィード(fixtures.dedupe)を登録するので、以降は「厳密なカウント」に依存しない
// (01〜06 のグループの後に置いてある理由)。
//
// 変数: maxId(v1 登録直後に挿入された2件の id の最大値)は、touch 経由の無駄な再取得を
// 挟んでから「本当に新しい記事の id が max_id+1 のまま(欠番なし)」を確認する最後の test() まで
// 複数の test() をまたいで参照するので、モジュールスコープの変数に置く
// (旧 Hurl の [Captures] expected_id、旧 bash の max_id 変数に相当)。
//
// 注意(fixture-files.ts と旧 bash 版の前提の違い): 旧 dedupe_no_304_e2e.sh の `touch` は
// 「hurl 呼び出しまでにかかる実行時間で mtime が(秒精度の)前回の値からずれる」という
// 暗黙の前提に頼っていた。support/fixture-files.ts の serveFixture/touchFixture は
// max(now, 現在の mtime + 1秒) で mtime を明示的に進めるため、その暗黙の前提はもう無い
// (同じ秒内に連続で呼んでも必ず ETag が変わることが保証される)。
import { test, expect } from '@playwright/test';
import { fixtureURL, fixtures } from '../../support/env';
import { registerFeed, listArticles, json } from '../../support/moka-api';
import { serveFixture, touchFixture } from '../../support/fixture-files';
import type { FeedRegistration } from '../../support/types';

test.describe.configure({ mode: 'serial' });

const dedupeFixtureUrl = fixtureURL(fixtures.dedupe.served);

let maxId: number;

test('1-3. v1(記事2件)を配信して初回登録し、挿入された2件の id の最大値を求める', async ({
	request
}) => {
	// 1. 配信内容を v1(記事2件)にする
	await test.step('1. 配信内容を v1(記事2件)にする', () => {
		serveFixture(fixtures.dedupe.v1, fixtures.dedupe.served);
	});

	// 2. 初回登録 — 2件挿入される(このフィード URL は初登場なので新規フィード = 201)
	await test.step('2. 初回登録 — 2件挿入される', async () => {
		const response = await registerFeed(request, dedupeFixtureUrl);
		expect(response.status(), await response.text()).toBe(201);
		const body = await json<FeedRegistration>(response);
		expect(body.inserted_articles, 'inserted_articles が2件であること').toBe(2);
	});

	// 3. 挿入された2件の id を取得(pubDate を2030年にしてあるので一覧の先頭に来る)
	await test.step('3. 挿入された2件の id を取得し、大きい方を max_id とする', async () => {
		const body = await listArticles(request, { limit: 2 });
		expect(body.articles, 'articles が2件であること').toHaveLength(2);
		const first = body.articles[0];
		const second = body.articles[1];
		expect(first, 'articles[0] が存在すること').toBeDefined();
		expect(second, 'articles[1] が存在すること').toBeDefined();
		maxId = Math.max(first!.id, second!.id);
	});
});

test('4-5. 内容を変えず touch するだけで再取得 — 条件付きGETを迂回して200が返っても挿入0件のまま(id を消費しない)', async ({
	request
}) => {
	// 4. 内容は変えずに mtime だけ更新 → nginx の ETag が変わり、条件付きGETが効かず 200 が返る
	await test.step('4. 内容は変えずに mtime だけ更新 → nginx の ETag が変わり、条件付きGETが効かず200が返る', () => {
		touchFixture(fixtures.dedupe.served);
	});

	// 5. 同じ内容で再登録 — 200 かつ inserted_articles == 0
	//    (事前チェックが無ければ ON CONFLICT で捨てられる2件分もシーケンスを消費してしまう)
	await test.step('5. 同じ内容で再登録 — 200 かつ inserted_articles == 0(事前チェックが無ければシーケンスを消費してしまう)', async () => {
		const response = await registerFeed(request, dedupeFixtureUrl);
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<FeedRegistration>(response);
		expect(
			body.inserted_articles,
			'inserted_articles が0件であること(sequence-burn regression が無いこと)'
		).toBe(0);
	});
});

test('6-7. 本当に新しい記事を1件追加した配信に差し替えて再取得 — 新規記事の id が max_id+1 と連番のまま(欠番なし)', async ({
	request
}) => {
	// 6. 本当に新しい記事を1件追加した配信に差し替えて再取得 — 次の新規記事の id が
	//    max_id+1 に連続していることを検証する(事前チェックが無ければ手順5で2つ欠番が
	//    生まれ、max_id+3 になっていたはず)
	await test.step('6. v2(3件目を追加)に差し替えて再取得 — 1件挿入される', async () => {
		serveFixture(fixtures.dedupe.v2, fixtures.dedupe.served);
		const response = await registerFeed(request, dedupeFixtureUrl);
		expect(response.status(), await response.text()).toBe(200);
		const body = await json<FeedRegistration>(response);
		expect(body.inserted_articles, 'inserted_articles が1件であること').toBe(1);
	});

	// 7. 条件付きGETを迂回した無駄な再取得の後でも、本当に新しい記事のIDが連番のまま
	//    (欠番が増えていない)ことを確認する(dedupe_no_304.hurl の最終アサーション相当)
	await test.step('7. 新しい記事の id が max_id+1 であること(欠番なし)', async () => {
		const body = await listArticles(request, { limit: 1 });
		const first = body.articles[0];
		expect(first, 'articles[0] が存在すること').toBeDefined();
		expect(first!.guid, 'articles[0].guid が "urn:moka-e2e-dedupe:3" であること').toBe(
			'urn:moka-e2e-dedupe:3'
		);
		expect(first!.id, 'articles[0].id が max_id+1 であること(欠番なし)').toBe(maxId + 1);
	});
});
