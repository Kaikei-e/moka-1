// セッション署名 cookie(ADR00021)の鋳造。
//
// 契約(core/internal/auth/session.go / plecto の session-auth フィルタと共有):
//   moka_session = v1.<exp_unix_ms>.<base64url_nopad(HMAC-SHA256(secret, "v1."+exp_unix_ms))>
// 鍵は secrets/session_hmac_key.txt の**内容を trim した文字列の UTF-8 バイト列**そのもの
// (hex デコードはしない)。edge の検証フィルタだけを相手にするので moka-core は要らない。

import { createHmac } from 'node:crypto';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { repoRoot } from './env';

const COOKIE_NAME = 'moka_session';
const VERSION = 'v1';

/** 共有シークレット(trim 済み)。未配線なら分かる形で落とす。 */
export function sessionSecret(): string {
	const file =
		process.env.SESSION_HMAC_KEY_FILE ?? path.join(repoRoot, 'secrets', 'session_hmac_key.txt');
	const secret = readFileSync(file, 'utf8').replace(/\s/g, '');
	if (secret === '') {
		throw new Error(`${file} が空です(secrets/README.md の初回セットアップを実行してください)`);
	}
	return secret;
}

/** 署名部 — base64url(パディング無し)。 */
function sign(payload: string, secret = sessionSecret()): string {
	return createHmac('sha256', secret).update(payload).digest('base64url');
}

/** `v1.<exp_unix_ms>` のペイロード部。 */
function payloadFor(expiresAtUnixMs: number): string {
	return `${VERSION}.${Math.trunc(expiresAtUnixMs)}`;
}

/** 正しく署名された cookie 値。`expiresAtUnixMs` が過去なら期限切れ cookie になる。 */
export function sessionCookieValue(expiresAtUnixMs: number): string {
	const payload = payloadFor(expiresAtUnixMs);
	return `${payload}.${sign(payload)}`;
}

/**
 * 改竄 cookie 値 — `expiresAtUnixMs` のペイロードに、別のペイロード
 * (`signedExpiresAtUnixMs`)の正しい署名を付け替える。署名検証で必ず落ちる。
 */
export function tamperedCookieValue(
	expiresAtUnixMs: number,
	signedExpiresAtUnixMs: number
): string {
	return `${payloadFor(expiresAtUnixMs)}.${sign(payloadFor(signedExpiresAtUnixMs))}`;
}

/** `Cookie:` ヘッダの値へ組み立てる。 */
export function cookieHeader(value: string): string {
	return `${COOKIE_NAME}=${value}`;
}

/** 有効 / 期限切れ / 改竄の3種をまとめて鋳造する(edge シナリオが使う)。 */
export function mintSessionCookies(now = Date.now(), lifetimeMs = 3600_000) {
	const validExp = now + lifetimeMs;
	const expiredExp = now - lifetimeMs;
	return {
		valid: sessionCookieValue(validExp),
		expired: sessionCookieValue(expiredExp),
		tampered: tamperedCookieValue(validExp, expiredExp)
	};
}
