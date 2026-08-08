import js from '@eslint/js';
import prettier from 'eslint-config-prettier';
import playwright from 'eslint-plugin-playwright';
import { defineConfig, globalIgnores } from 'eslint/config';
import globals from 'globals';
import ts from 'typescript-eslint';

export default defineConfig(
	globalIgnores(['test-results/', 'playwright-report/', 'reports/', 'fixtures/', 'mock-llm/']),
	js.configs.recommended,
	ts.configs.recommended,
	prettier,
	{
		languageOptions: { globals: { ...globals.node } },
		rules: {
			// typescript-eslint は TS プロジェクトで no-undef を無効にすることを推奨している
			'no-undef': 'off'
		}
	},
	{
		files: ['tests/**/*.spec.ts', 'tests/**/*.setup.ts'],
		...playwright.configs['flat/recommended'],
		rules: {
			...playwright.configs['flat/recommended'].rules,
			// DB 依存の直列シナリオなので、条件分岐やループを含む手順が本質的に必要になる箇所がある
			// (フィード登録の冪等性・ポーリング等)。ここは individual に無効化せず全体で許可する
			'playwright/no-conditional-in-test': 'off',
			// setup プロジェクト(health gate)は expect を持たない
			'playwright/expect-expect': 'off'
		}
	}
);
