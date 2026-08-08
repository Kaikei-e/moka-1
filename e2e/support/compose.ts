// docker compose(compose.yaml + compose.e2e.yaml オーバーレイ)への操作。
//
// Hurl では表現できず シェルスクリプトに逃がしていた手順 — 「llm を実際に止める」
// 「fetch_interval_seconds を DB 直接操作で縮める」— を Playwright のテスト本体から
// 行うための最小の口。テスト用の特権操作であることを明示するため、ここ以外から
// child_process を呼ばない。
//
// execFileSync(argv 配列)を使い execSync(シェル経由)は使わない — フィクスチャ URL や
// 環境変数がシェルに解釈される余地を残さない。

import { execFileSync } from 'node:child_process';
import { repoRoot } from './env';

const composeFiles = ['-f', 'compose.yaml', '-f', 'compose.e2e.yaml'];

export type ComposeOptions = {
	/** true なら失敗しても例外にせず空文字を返す(後始末の安全網用)。 */
	allowFail?: boolean;
	timeoutMs?: number;
};

/** docker compose サブコマンドを実行し、stdout を返す。 */
export function compose(args: string[], options: ComposeOptions = {}): string {
	const { allowFail = false, timeoutMs = 120_000 } = options;
	try {
		return execFileSync('docker', ['compose', ...composeFiles, ...args], {
			cwd: repoRoot,
			encoding: 'utf8',
			timeout: timeoutMs,
			stdio: ['ignore', 'pipe', 'pipe']
		});
	} catch (error) {
		if (allowFail) return '';
		throw new Error(`docker compose ${args.join(' ')} に失敗しました`, { cause: error });
	}
}

/** サービスを停止する(コンテナは残す — start で再開できる)。 */
export function stopService(service: string, options: ComposeOptions = {}): void {
	compose(['stop', service], options);
}

/** stop したサービスを再開する。冪等(既に動いていれば no-op)。 */
export function startService(service: string, options: ComposeOptions = {}): void {
	compose(['start', service], options);
}

/**
 * e2e-db に SQL を投げる(テスト専用の DB 直接操作)。
 * API に導線が無い設定(feeds.fetch_interval_seconds)を縮めるためだけに使う。
 */
export function psql(sql: string, options: ComposeOptions = {}): string {
	return compose(['exec', '-T', 'e2e-db', 'psql', '-U', 'moka', '-d', 'moka', '-c', sql], options);
}
