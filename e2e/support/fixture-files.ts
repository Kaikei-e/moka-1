// 配信中フィクスチャの差し替え。e2e/fixtures/ は e2e-fixtures(nginx)へ ro bind mount
// されているので、ホスト側でファイルを書き換えるとそのまま配信内容が変わる。
//
// nginx の ETag は「mtime(秒) + Content-Length」から決まる。内容を変えずに mtime だけ
// 進めれば「サーバが条件付き GET を無視して 200 を返す」状況を再現でき、内容ごと
// 差し替えれば通常の更新になる。

import { copyFileSync, statSync, utimesSync } from 'node:fs';
import path from 'node:path';
import { e2eRoot } from './env';

const fixturesDir = path.join(e2eRoot, 'fixtures');

/** `sourceName` の内容を配信実体 `servedName` へコピーする(= 配信内容の差し替え)。 */
export function serveFixture(sourceName: string, servedName: string): void {
	copyFileSync(path.join(fixturesDir, sourceName), path.join(fixturesDir, servedName));
	bumpMtime(servedName);
}

/**
 * 内容は変えずに mtime だけ進める(`touch` 相当)。nginx の ETag が変わるので、
 * 次回取得は 304 にならず本文つき 200 が返る。
 */
export function touchFixture(servedName: string): void {
	bumpMtime(servedName);
}

/**
 * mtime を「現在時刻」か「現在の mtime + 1 秒」の大きい方へ進める。
 * ETag は秒精度なので、同じ秒内に連続して呼ばれても必ず値が変わることを保証する。
 */
function bumpMtime(servedName: string): void {
	const file = path.join(fixturesDir, servedName);
	const current = statSync(file).mtimeMs;
	const next = new Date(Math.max(Date.now(), current + 1000));
	utimesSync(file, next, next);
}
