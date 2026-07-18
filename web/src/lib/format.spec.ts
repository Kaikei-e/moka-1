import { describe, expect, it } from 'vitest';
import { formatDate, formatRelativeTime } from './format';

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

describe('formatRelativeTime', () => {
	const now = new Date('2026-07-18T12:00:00Z');

	it('says たった今 within the first minute', () => {
		expect(formatRelativeTime('2026-07-18T11:59:30Z', now)).toBe('たった今');
		expect(formatRelativeTime('2026-07-18T12:00:00Z', now)).toBe('たった今');
	});

	it('rounds a future timestamp (clock skew) down to たった今', () => {
		expect(formatRelativeTime('2026-07-18T12:05:00Z', now)).toBe('たった今');
	});

	it('counts minutes below an hour', () => {
		expect(formatRelativeTime('2026-07-18T11:59:00Z', now)).toBe('1分前');
		expect(formatRelativeTime('2026-07-18T11:15:00Z', now)).toBe('45分前');
	});

	it('counts hours below a day', () => {
		expect(formatRelativeTime('2026-07-18T11:00:00Z', now)).toBe('1時間前');
		expect(formatRelativeTime('2026-07-17T12:30:00Z', now)).toBe('23時間前');
	});

	it('says 昨日 for one day ago', () => {
		expect(formatRelativeTime('2026-07-17T11:00:00Z', now)).toBe('昨日');
	});

	it('counts days up to a week', () => {
		expect(formatRelativeTime('2026-07-16T11:00:00Z', now)).toBe('2日前');
		expect(formatRelativeTime('2026-07-11T11:00:00Z', now)).toBe('7日前');
	});

	it('switches to an absolute date beyond a week', () => {
		expect(formatRelativeTime('2026-07-10T11:00:00Z', now)).toBe('2026年7月10日');
	});

	it('returns an empty string for null and garbage input', () => {
		expect(formatRelativeTime(null, now)).toBe('');
		expect(formatRelativeTime('not-a-date', now)).toBe('');
	});
});
