// llm 完全停止時のフェイルソフト検証(M2): e2e-llm-mock を実際に止めて、
//   - 検索がテキスト側単独で 200 を返し続けること(tenets §2-6 — 増強は死んでも読める)
//   - Q&A が SSE の error イベント(HTTP 200 のまま)で静かに失敗すること
// を検証する。停止中は enrich.Scheduler の埋め込み/要約も失敗 attempt を積む
// (バックオフで回復する)ため、他シナリオへの波及を避けて API 層 spec 群の一番最後(12-)に置く。
// 終了時(失敗時含む)に必ず e2e-llm-mock を再開する(旧 bash の `trap ... EXIT` に対応 —
// afterAll は失敗時も走る。Ctrl-C(SIGINT)だけは afterAll も teardown プロジェクトも走らないので、
// その保険は global-teardown.ts 側に別途置いてある — 調査ダイジェスト §8.8)。
// 移行元: hurl/core/rag_failsoft_e2e.sh + hurl/core/rag_failsoft.hurl
//
// 前提: フレッシュ DB + 記事が投入済み(01-feeds-and-articles.spec.ts 等)であること。
// なお冒頭の health gate(rag_failsoft.hurl #1)は tests/setup/core-health.setup.ts へ集約
// **しない** — setup プロジェクトは llm を止める前に走るので「moka-core 自体は llm 停止に
// 関係なく生きている(コアパスは無傷)」という別の主張になるため、停止後の test として残す。
//
// 手順の順序は旧 bash のまま(1. 記事 id を先に取る → 2. llm を止める → 3. 検証)。
// docker compose 操作(stopService)は test 本体から行う: test.beforeAll では { request }
// フィクスチャを使えず(Playwright がフック終了時に dispose し、後続の test で使うと
// 「Fixture { request } from beforeAll cannot be reused in a test.」で落ちる —
// 調査ダイジェスト §0-3 / 共通ルール #4)、「記事 id を先に取ってから止める」順序を
// beforeAll では表現できないため。再開だけは HTTP を伴わないので afterAll に置ける。
//
// 変数: articleId は「id を取得する」test() から Q&A の test() まで参照するので、
// モジュールスコープの変数に置く(旧 bash の article_id 変数に相当)。
import { test, expect } from '@playwright/test';
import { firstArticleId, json, postSse } from '../../support/moka-api';
import { expectEventStream, sseEventNames, sseData } from '../../support/sse';
import { stopService, startService } from '../../support/compose';
import type { SearchResponse } from '../../support/types';

test.describe.configure({ mode: 'serial' });

// 終了時に必ず再開する。stop に到達していなくても start は冪等なので無条件に呼ぶ
// (旧 bash: trap '"${COMPOSE[@]}" start e2e-llm-mock >/dev/null 2>&1 || true' EXIT)
test.afterAll(() => {
	startService('e2e-llm-mock', { allowFail: true });
});

let articleId: number;

test('1-2. 質問対象の記事 id を先に取っておき、llm(モック)を止める', async ({ request }) => {
	await test.step('1. 質問対象の記事 id を先に取っておく(どの記事でもよい — 存在することだけが前提)', async () => {
		// 旧 bash は curl + jq で取れなければ「no articles found (run after feeds_and_articles.hurl)」
		// として exit 1 していた。firstArticleId が「記事が1件以上あること」を主張する
		articleId = await firstArticleId(request);
	});

	await test.step('2. llm(モック)を止める。以降 moka-core からの補完・埋め込みは接続エラーになる', () => {
		stopService('e2e-llm-mock');
	});
});

// 3. health gate — moka-core 自体は llm 停止に関係なく生きている(コアパスは無傷)
test('3. health gate — moka-core 自体は llm 停止に関係なく生きている(コアパスは無傷)', async ({
	request
}) => {
	// 移行元 rag_failsoft.hurl #1。Hurl: retry: 10, retry-interval: 2000 → 10回の追加試行 = 最大20秒
	await expect
		.poll(async () => (await request.get('/healthz')).status(), {
			intervals: [2000],
			timeout: 20_000
		})
		.toBe(200);
});

// 4. llm 停止時も検索は 200 — pg_trgm テキスト側単独への縮退(RRF のベクトル側は空)
// (移行元 rag_failsoft.hurl #2)
test('4. llm 停止時も検索は 200 — pg_trgm テキスト側単独への縮退(RRF のベクトル側は空)', async ({
	request
}) => {
	const response = await request.get('/api/v1/search', { params: { q: 'Third article' } });
	expect(response.status(), await response.text()).toBe(200);
	const body = await json<SearchResponse>(response);
	expect(body.items.length, 'items が1件以上であること').toBeGreaterThanOrEqual(1);
	const first = body.items[0];
	expect(first, 'items[0] が存在すること').toBeDefined();
	expect(first!.title, 'items[0].title が "Third article" であること').toBe('Third article');
});

// 4b. ヒットなしは 200 の空配列(null でなく [] — 一覧 API と同じ契約)。
// 埋め込みが効いている通常時はベクトル近傍が必ず何か返す(top-k に閾値は無い)ため、
// この契約はクエリ埋め込みが縮退で空になる llm 停止時にだけ決定的に観測できる
// (03-search.spec.ts #6 の注記と対。移行元 rag_failsoft.hurl #2b)
test('4b. ヒットなしは 200 の空配列(null でなく [])', async ({ request }) => {
	const response = await request.get('/api/v1/search', { params: { q: 'qqqzzzxxxvvv' } });
	expect(response.status(), await response.text()).toBe(200);
	const body = await json<SearchResponse>(response);
	expect(body.items, 'items が空配列であること').toHaveLength(0);
});

// 5. Q&A は sources(文脈選定はテキスト検索で成立)まで届いた後、回答生成で llm に
// 到達できず error イベントになる。done は出ない。message はドメイン sentinel の写像
// (移行元 rag_failsoft.hurl #3)
test('5. Q&A は sources まで届いた後、回答生成で llm に到達できず error イベントになる(done は出ない)', async ({
	request
}) => {
	const response = await postSse(request, `/api/v1/articles/${articleId}/qa`, {
		question: 'llm が居ないとどうなりますか'
	});
	expect(response.status(), await response.text()).toBe(200);
	expectEventStream(response);

	const body = await response.text();

	// event: sources / event: error / (event: done を含まない)は、旧 Hurl の body contains の
	// 素朴な部分文字列一致でなく sseEventNames(body) によるイベント名配列で表明する
	// (調査ダイジェスト §8.1、共通ルール #7 — body not contains の否定はここに残す)
	const eventNames = sseEventNames(body);
	expect(eventNames, 'event: sources を含むこと(文脈選定はテキスト検索で成立する)').toContain(
		'sources'
	);
	expect(eventNames, 'event: error を含むこと').toContain('error');
	expect(
		eventNames,
		'event: done を含まないこと(回答生成で llm に到達できず終了する)'
	).not.toContain('done');
	// 旧 Hurl の `body not contains "event: done"` — パース済みイベント名と生ボディの両方で否定する
	expect(body, 'body が "event: done" を含まないこと').not.toContain('event: done');
	// sources が error より先に届く(コメントの因果順「sources まで届いた後、…error イベントになる」
	// の主張 — 旧 Hurl は body contains の部分文字列一致でしか書けなかった箇所。移行による正しい強化)
	expect(eventNames.indexOf('sources'), 'sources が error より先に来ること').toBeLessThan(
		eventNames.indexOf('error')
	);

	expect(body, 'body が "llm unavailable" を含むこと').toContain('llm unavailable');
	const errorEvent = sseData<{ message: string }>(body, 'error');
	expect(errorEvent, 'error イベントの data が JSON としてパースできること').toBeDefined();
	expect(
		errorEvent!.message,
		'error イベントの message が "llm unavailable"(qaErrorMessage の写像)であること'
	).toBe('llm unavailable');
});
