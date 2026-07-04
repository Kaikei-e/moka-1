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
	// モバイル導線(記事リスト⇄読書ビューのプッシュ遷移)は 900px 未満のビューポートでのみ
	// 意味を持つため専用プロジェクトに分離する。既存シナリオ(desktop)は従来どおりデフォルト
	// ビューポートのまま
	projects: [
		{
			name: 'desktop',
			testIgnore: /03-mobile-navigation\.spec\.ts/
		},
		{
			name: 'mobile',
			testMatch: /03-mobile-navigation\.spec\.ts/,
			use: { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true }
		}
	],
	// CI では list に加えて html レポートを出す(失敗時に artifact として回収する)
	reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : [['list']]
});
