// Hurl の述語に対応する小さな検証ヘルパ。
// Playwright の expect は汎用マッチャしか持たないので、Hurl の `isIsoDate` /
// `isInteger` / `isFloat` に相当する判定だけをここで一箇所に閉じ込める。

import { expect } from '@playwright/test';

/**
 * RFC3339 / ISO8601 の日時文字列か(Hurl の `isIsoDate` 相当)。
 * Go の time.Time が JSON へ書き出す形(例 2026-07-01T09:00:00Z、
 * 2026-07-01T09:00:00.123456+09:00)を受ける。
 */
const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

/** Hurl の `jsonpath "$.x" isIsoDate` 相当。 */
export function expectIsoDate(value: unknown, label = 'value'): void {
	expect(typeof value, `${label} は文字列であること`).toBe('string');
	expect(value as string, `${label} は ISO8601 であること`).toMatch(ISO_DATE_RE);
	expect(Number.isNaN(Date.parse(value as string)), `${label} はパース可能であること`).toBe(false);
}

/** Hurl の `jsonpath "$.x" isInteger` 相当。 */
export function expectInteger(value: unknown, label = 'value'): void {
	expect(Number.isInteger(value), `${label} は整数であること`).toBe(true);
}

/**
 * Hurl の `jsonpath "$.x" isFloat` 相当。Hurl の isFloat は「JSON の数値であって
 * 整数でない」ではなく「浮動小数点数として読める数値」を指すので、有限数であることを見る。
 */
export function expectFloat(value: unknown, label = 'value'): void {
	expect(typeof value, `${label} は数値であること`).toBe('number');
	expect(Number.isFinite(value as number), `${label} は有限の数であること`).toBe(true);
}

/** Hurl の `jsonpath "$.x" matches /.+/` 相当(空でない文字列)。 */
export function expectNonEmptyString(value: unknown, label = 'value'): void {
	expect(typeof value, `${label} は文字列であること`).toBe('string');
	expect((value as string).length, `${label} は空でないこと`).toBeGreaterThan(0);
}
