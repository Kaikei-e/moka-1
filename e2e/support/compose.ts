// docker compose(compose.yaml + compose.e2e.yaml オーバーレイ)への操作。
//
// Hurl では表現できず シェルスクリプトに逃がしていた手順 — 「llm を実際に止める」
// 「fetch_interval_seconds を DB 直接操作で縮める」— を Playwright のテスト本体から
// 行うための最小の口。テスト用の特権操作であることを明示するため、ここ以外から
// child_process を呼ばない。

import { execFileSync } from 'node:child_process';
import { repoRoot } from './env';

const composeFiles = ['-f', 'compose.yaml', '-f', 'compose.e2e.yaml'];

/** docker compose サブコマンドを実行し、stdout を返す。失敗すれば例外を投げる。 */
export function compose(args: string[], timeoutMs = 120_000): string {
	return execFileSync('docker', ['compose', ...composeFiles, ...args], {
		cwd: repoRoot,
		encoding: 'utf8',
		timeout: timeoutMs,
		stdio: ['ignore', 'pipe', 'pipe']
	});
}

/** サービスを停止する(コンテナは残す — start で再開できる)。 */
export function stopService(service: string): void {
	compose(['stop', service]);
}

/** stop したサービスを再開する。 */
export function startService(service: string): void {
	compose(['start', service]);
}

/**
 * e2e-db に SQL を投げる(テスト専用の DB 直接操作)。
 * API に導線が無い設定(feeds.fetch_interval_seconds)を縮めるためだけに使う。
 */
export function psql(sql: string): string {
	return compose(['exec', '-T', 'e2e-db', 'psql', '-U', 'moka', '-d', 'moka', '-c', sql]);
}
