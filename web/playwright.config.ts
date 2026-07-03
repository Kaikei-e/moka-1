import { defineConfig } from '@playwright/test';

// 実スタック(compose.yaml + compose.e2e.yaml)に対して走る。webServer は起動しない。
// 前提: フレッシュ DB。手順は e2e/README.md 参照
export default defineConfig({
	testDir: 'tests/e2e',
	fullyParallel: false,
	workers: 1, // DB 依存シナリオ(登録 → 一覧反映)なので直列
	use: {
		baseURL: process.env.E2E_WEB_URL ?? 'http://localhost:3000',
		trace: 'retain-on-failure'
	},
	// CI では list に加えて html レポートを出す(失敗時に artifact として回収する)
	reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : [['list']]
});
