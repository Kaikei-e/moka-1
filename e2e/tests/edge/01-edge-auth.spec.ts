// エッジ(Plecto Phase 2)のセッション認証 + レート制限フィルタ(ADR00021、tenets §3.5)
// 移行元: hurl/edge/edge_auth_e2e.sh + hurl/edge/edge_auth.hurl
//
// playwright.edge.config.ts 側で走る(baseURL = edgeBaseURL = https://localhost、
// ignoreHTTPSErrors: true — 自己署名証明書)。API 層(playwright.config.ts)とは別 config。
// health gate は tests/setup/edge-health.setup.ts(setup プロジェクト)に集約済みなのでここでは打たない
// (エッジ → moka-web まで生きていることの確認 = 旧 edge_auth.hurl #1。/healthz ルートは
// セッション認証の除外パスなので cookie 無しで通る)。
//
// セッション cookie(有効 / 期限切れ / 改竄)は旧 bash の openssl パイプラインの代わりに
// support/session-cookie.ts の mintSessionCookies()(node:crypto の HMAC-SHA256、secrets の
// 共有シークレットから鋳造)で作る。契約は moka-core / plecto と同一(ADR00021)で、Node と
// openssl がバイト一致することは調査ダイジェスト §8.4 で実測済み — openssl 呼び出しは不要になった。
// 鍵ファイルが無い/空なら sessionSecret() が secrets/README.md へ誘導して落ちる
// (旧 bash の `if [ ! -s "$KEY_FILE" ]` チェックに対応)。
//
// **すべての GET に maxRedirects: 0 を付ける**: Hurl はリダイレクトを一切追わないので、
// `HTTP 200` は「その GET そのものが 200」という主張である。Playwright の既定は「最大20まで
// 追う」なので、付け忘れると 302 → 追跡先の 200 で静かに通ってしまい Hurl より弱くなる
// (調査ダイジェスト §0-2 / §8.3、共通ルール #2)。バースト(手順8)でも、追跡した分だけ
// 余計にトークンを消費してバケットの観測がずれる。なお maxRedirects: 0 は
// 「リダイレクト応答をそのまま返す」であって例外ではない(playwright-core 1.62 で内部的に
// -1 に写像され、リダイレクト追跡の分岐に入らない)ので 302 をそのまま観測できる。
//
// 末尾のバースト(手順8/8b)が per-IP の /auth バケット(capacity 10, refill 1/s)を空にするため、
// このファイルは edge 検証の最後に置く。連続実行する場合は10秒ほど空けること。
// [Options] repeat: 12 は逐次 for ループで再現する(並列化しない — Promise.all は同時接続数の
// 上限が無くソケットを N 本開くうえ、結果配列の順序はサーバの到着順を意味せずトークンバケットの
// 観測が不安定になる。調査ダイジェスト §8.6、共通ルール #6)。
//
// APIResponse.securityDetails()(v1.61+ で終端証明書を検証できる)は意図的に足さない —
// 移行元 edge_auth.hurl に無い主張であり、--insecure は「検証をスキップする」という意味であって
// 「検証して確かめる」という意味ではないため(担当グループの注意書きに従う)。
//
// 変数: valid/expired/tampered の3種の cookie は複数 test() から参照するのでモジュールスコープの
// 定数に置く(旧 Hurl の --variable valid_cookie / expired_cookie / tampered_cookie に相当)。
import { test, expect } from '@playwright/test';
import type { APIRequestContext, APIResponse } from '@playwright/test';
import { mintSessionCookies, cookieHeader } from '../../support/session-cookie';

test.describe.configure({ mode: 'serial' });

const HTML_ACCEPT = 'text/html,application/xhtml+xml';
const JSON_ACCEPT = 'application/json';

// 鋳造はモジュール読み込み時ではなく最初に使われた時に行う(メモ化)。モジュールスコープで
// 即時に呼ぶと、secrets/session_hmac_key.txt が無いときに「テストの失敗」ではなく
// 「ファイルのロードエラー」になり、--list すら通らなくなる
let mintedCookies: ReturnType<typeof mintSessionCookies> | undefined;
function cookies(): ReturnType<typeof mintSessionCookies> {
	mintedCookies ??= mintSessionCookies();
	return mintedCookies;
}

/**
 * GET / — Accept と Cookie を指定できる薄いラッパ(このファイル専用のローカルヘルパ。
 * support/ へ昇格させるほどの再利用先が今のところ無いのでここに閉じ込める)。
 * リダイレクトは追わない(Hurl と同じ観測モデル)。
 */
async function getRoot(
	request: APIRequestContext,
	options: { accept: string; cookie?: string }
): Promise<APIResponse> {
	const headers: Record<string, string> = { Accept: options.accept };
	if (options.cookie !== undefined) headers.Cookie = options.cookie;
	return request.get('/', { headers, maxRedirects: 0 });
}

/** GET /auth/login — Accept は任意(旧 Hurl #8 / #8b はヘッダ無しで叩いている)。 */
async function getAuthLogin(
	request: APIRequestContext,
	options: { accept?: string } = {}
): Promise<APIResponse> {
	const headers: Record<string, string> = {};
	if (options.accept !== undefined) headers.Accept = options.accept;
	return request.get('/auth/login', { headers, maxRedirects: 0 });
}

// 2. 未認証の text/html GET はログイン画面へ 302(ブラウザのページ遷移の作法)
test('2. 未認証の text/html GET はログイン画面へ302', async ({ request }) => {
	const response = await getRoot(request, { accept: HTML_ACCEPT });
	expect(response.status(), await response.text()).toBe(302);
	expect(response.headers()['location'], 'Location が "/auth/login" を含むこと').toContain(
		'/auth/login'
	);
});

// 3. 未認証の非html(API的アクセス)は401 + WWW-Authenticate(RFC 9110)
test('3. 未認証の非html アクセスは401 + WWW-Authenticate', async ({ request }) => {
	const response = await getRoot(request, { accept: JSON_ACCEPT });
	expect(response.status(), await response.text()).toBe(401);
	expect(response.headers()['www-authenticate'], 'WWW-Authenticate が存在すること').toBeDefined();
});

// 4. 改竄cookieは無効(fail-closed — 署名が合わなければ未認証と同じ)
test('4. 改竄cookieは無効(fail-closed)', async ({ request }) => {
	const response = await getRoot(request, {
		accept: JSON_ACCEPT,
		cookie: cookieHeader(cookies().tampered)
	});
	expect(response.status(), await response.text()).toBe(401);
});

// 5. 署名は正しいが期限切れのcookieも無効(exp_unix_ms > now の検査)
test('5. 署名は正しいが期限切れのcookieも無効', async ({ request }) => {
	const response = await getRoot(request, {
		accept: JSON_ACCEPT,
		cookie: cookieHeader(cookies().expired)
	});
	expect(response.status(), await response.text()).toBe(401);
});

// 6. 有効な署名cookieは通る — moka-webのSSRページが返る
test('6. 有効な署名cookieは通る(moka-webのSSRページが返る)', async ({ request }) => {
	const response = await getRoot(request, {
		accept: HTML_ACCEPT,
		cookie: cookieHeader(cookies().valid)
	});
	expect(response.status(), await response.text()).toBe(200);
	expect(response.headers()['content-type'], 'Content-Type が text/html を含むこと').toContain(
		'text/html'
	);
});

// 7. /auth はセッション認証の除外パス(未認証者の入口)— cookie無しで200
test('7. /auth/login はセッション認証の除外パス(cookie無しで200)', async ({ request }) => {
	const response = await getAuthLogin(request, { accept: HTML_ACCEPT });
	expect(response.status(), await response.text()).toBe(200);
});

// 8-8b. /auth はバケットが厳しい(capacity 10, refill 1/s)— バーストで尽きる。
// ここまでで /auth は1回消費済み(手順7)。素早く12回叩けばバケットは必ず空になる。
// per-IP バケットを汚すので、このシナリオはファイル末尾・edge 検証の最後に置く。
// 8 と 8b はバケット状態が線形に依存するので同一 test() 内の test.step に分ける
test('8-8b. /auth バースト12連打でバケットが尽き、次の1発は429 + Retry-After', async ({
	request
}) => {
	// 8. [Options] repeat: 12, HTTP *(ステータス不問)— ここでは「バケットを空にする」ことだけが
	// 目的で、個々のレスポンスのステータスは主張しない
	await test.step('8. /auth バケットを12連打で空にする(ステータス不問)', async () => {
		for (let i = 0; i < 12; i++) {
			await getAuthLogin(request);
		}
	});

	// 8b. 尽きた直後の1発は429(Retry-Afterで補充を伝える)
	await test.step('8b. 尽きた直後の1発は429(Retry-Afterで補充を伝える)', async () => {
		const response = await getAuthLogin(request);
		expect(response.status(), await response.text()).toBe(429);
		expect(response.headers()['retry-after'], 'Retry-After が存在すること').toBeDefined();
	});
});

// 9. バケットは経路別 — /auth が429でも通常経路(capacity 100)は影響を受けない
test('9. バケットは経路別 — /authが429でも通常経路は影響を受けない', async ({ request }) => {
	const response = await getRoot(request, {
		accept: HTML_ACCEPT,
		cookie: cookieHeader(cookies().valid)
	});
	expect(response.status(), await response.text()).toBe(200);
});
