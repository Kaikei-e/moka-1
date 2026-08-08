import { defineConfig } from '@playwright/test';
import { coreBaseURL } from './support/env';

// moka-core の API 層 E2E。実スタック(compose.yaml + compose.e2e.yaml)に対して走る。
// webServer は起動しない。前提: フレッシュ DB。手順は e2e/README.md 参照。
//
// エッジ(Plecto)の E2E は本番スタック側の配線が要るので playwright.edge.config.ts に分けて
// あり、`pnpm test` では走らない。UI 層は web/tests/e2e/(別の Playwright プロジェクト)。
export default defineConfig({
	testDir: 'tests',

	// DB 依存の直列シナリオ。ファイル名の連番がそのまま実行順になる(Playwright は
	// パス順にファイルを実行する)ので、順序の前提は各 spec 冒頭のコメントに書く
	fullyParallel: false,
	workers: 1,

	// 再試行は禁止。途中まで進んだ DB 状態のうえで最初からやり直しても意味が無く、
	// 「フレッシュ DB でちょうど N 件」の主張を静かに壊す
	retries: 0,

	forbidOnly: !!process.env.CI,

	// 12-rag-failsoft.spec.ts が止めた e2e-llm-mock を、Ctrl-C(SIGINT)で中断した場合でも
	// 確実に再開するための安全網。afterAll と teardown プロジェクトは SIGINT では走らない
	globalTeardown: './global-teardown.ts',

	// enrich.Scheduler の自動濃縮・埋め込み待ちを含むシナリオがあるため長め。
	// さらに長いものは spec 側で test.setTimeout する
	timeout: 120_000,
	expect: {
		timeout: 15_000,
		// expect.toPass() の既定タイムアウトは 0(無限)で expect.timeout を見ない。
		// 書き忘れがそのままハングにならないよう、有限の既定をここで与える
		toPass: { timeout: 60_000, intervals: [500, 1000, 2000] }
	},

	use: {
		baseURL: coreBaseURL,
		trace: 'retain-on-failure'
	},

	projects: [
		{
			// moka-core が起動しきるまでの health gate(旧 hurl 各ファイル冒頭の retry 付き
			// GET /healthz を1回に集約したもの)
			name: 'setup',
			testDir: 'tests/setup',
			testMatch: /core-health\.setup\.ts/
		},
		{
			name: 'api',
			testDir: 'tests/api',
			dependencies: ['setup']
		}
	],

	// CI では list に加えて html / junit を出す(失敗時に artifact として回収する)
	reporter: process.env.CI
		? [['list'], ['html', { open: 'never' }], ['junit', { outputFile: 'reports/junit.xml' }]]
		: [['list']]
});
