// 常駐スケジューラ(tenets §3.2 の常駐エージェントループ step1)が、人間・API起点の操作
// 無しに自律的にフィードを再取得することを検証する(移行元: scheduler_e2e.sh +
// scheduler_poll.hurl)。Hurl の静的アサーションだけでは「DB を直接いじる」「配信内容を
// 差し替える」という手順を表現できないため旧シェルは curl + psql でオーケストレーションし、
// 最後だけ Hurl(retry付きポーリング)に委ねていた。Playwright ではそれを test 本体から
// 直接行う(DB 直接操作は support/compose.ts の psql、フィクスチャ差し替えは
// support/fixture-files.ts の serveFixture — どちらもテスト専用の特権操作)。
//
// 前提: フレッシュ DB。health gate は tests/setup/core-health.setup.ts(setup プロジェクト)に
// 集約済みなのでここでは打たない。09-enrich.spec.ts の後・11-qa-context-relevance.spec.ts の
// 前に置く(独自フィードを登録し、DB を直接いじる。e2e/README.md の実行順序表を参照)。
//
// 変数: feedId はモジュールスコープに保持し、test.describe.configure({ mode: 'serial' }) の下で
// 後続 test() へ引き継ぐ(旧シェルの feed_id 相当)。newGuid は scheduler_poll.hurl の
// new_guid 変数(urn:moka-e2e-sched:2)をそのまま定数化したもの。
import { test, expect } from '@playwright/test';
import { fixtureURL, fixtures } from '../../support/env';
import { registerFeedOk, listArticles } from '../../support/moka-api';
import { psql } from '../../support/compose';
import { serveFixture } from '../../support/fixture-files';

test.describe.configure({ mode: 'serial' });

/** scheduler_poll.hurl の new_guid 変数。v2 差し替え後にのみ現れる記事の guid。 */
const newGuid = 'urn:moka-e2e-sched:2';

let feedId: number;

test('1-4. フィードを v1 で登録し、取得間隔を短縮してから配信を v2 に差し替える', async ({
	request
}) => {
	await test.step('1. 配信内容を v1(記事1件)にする', () => {
		serveFixture(fixtures.scheduler.v1, fixtures.scheduler.served);
	});

	await test.step('2. フィードを登録(同期・API起点。ここまではユーザー操作の代替)', async () => {
		const { feed } = await registerFeedOk(request, fixtureURL(fixtures.scheduler.served));
		feedId = feed.id;
	});

	await test.step(
		'3. このフィードだけ取得間隔を短縮する(テスト専用のDB直接操作 — fetch_interval_seconds を' +
			'APIから設定する導線は今回のスコープ外)',
		() => {
			psql(`UPDATE feeds SET fetch_interval_seconds = 2 WHERE id = ${feedId};`);
		}
	);

	await test.step(
		'4. 配信内容を v2(記事2件)に差し替える。ETag/Last-Modified が変わるので次回取得は' +
			'304にならず新規記事が入る。以後はスケジューラの自律動作のみが頼り',
		() => {
			serveFixture(fixtures.scheduler.v2, fixtures.scheduler.served);
		}
	);
});

test('5. スケジューラが自律的に再取得して新記事が現れるまでポーリングする(人間・API操作は無し)', async ({
	request
}) => {
	// Hurl: retry: 20, retry-interval: 1000 → 20回の追加試行 = 最大20秒
	await expect(async () => {
		const { articles } = await listArticles(request, { limit: 50 });
		expect(
			articles.map((a) => a.guid),
			`新記事 ${newGuid} が現れること(常駐スケジューラが自律的に再取得した証拠)`
		).toContain(newGuid);
	}).toPass({ intervals: [1000], timeout: 20_000 });
});
