import { describe, expect, it } from 'vitest';
import { formatDate } from './format';

describe('formatDate', () => {
	it('formats an ISO timestamp as a Japanese calendar date', () => {
		expect(formatDate('2026-07-01T09:00:00Z')).toBe('2026年7月1日');
	});

	it('returns an empty string for null (published_at is nullable)', () => {
		expect(formatDate(null)).toBe('');
	});

	it('returns an empty string for garbage input', () => {
		expect(formatDate('not-a-date')).toBe('');
	});
});
