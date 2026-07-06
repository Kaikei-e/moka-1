import { describe, expect, it } from 'vitest';
import { isSafeExternalUrl } from './url';

// フィード由来の URL は信用しない — href として描画してよいのは http/https のみ
describe('isSafeExternalUrl', () => {
	it('accepts http and https URLs', () => {
		expect(isSafeExternalUrl('http://example.com/a')).toBe(true);
		expect(isSafeExternalUrl('https://example.com/a?b=c#d')).toBe(true);
	});

	it('rejects javascript: URLs, including case and whitespace variants', () => {
		expect(isSafeExternalUrl('javascript:alert(1)')).toBe(false);
		expect(isSafeExternalUrl('JaVaScRiPt:alert(1)')).toBe(false);
		expect(isSafeExternalUrl('  javascript:alert(1)')).toBe(false);
	});

	it('rejects data: URLs', () => {
		expect(isSafeExternalUrl('data:text/html,<script>alert(1)</script>')).toBe(false);
	});

	it('rejects relative paths and garbage', () => {
		expect(isSafeExternalUrl('/articles/7')).toBe(false);
		expect(isSafeExternalUrl('example.com/no-scheme')).toBe(false);
		expect(isSafeExternalUrl('not a url')).toBe(false);
		expect(isSafeExternalUrl('')).toBe(false);
	});
});
