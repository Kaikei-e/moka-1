import { describe, expect, it } from 'vitest';
import { toParagraphs } from './article-text';

describe('toParagraphs', () => {
	it('splits plain text on blank lines', () => {
		expect(toParagraphs('one\n\ntwo\n\nthree')).toEqual(['one', 'two', 'three']);
	});

	it('strips html tags so external markup never reaches the dom', () => {
		expect(toParagraphs('<p>hello <b>world</b></p><script>alert(1)</script>')).toEqual([
			'hello world'
		]);
	});

	it('turns block-level tags into paragraph breaks', () => {
		expect(toParagraphs('<p>first</p><p>second</p>')).toEqual(['first', 'second']);
	});

	it('drops empty paragraphs and trims whitespace', () => {
		expect(toParagraphs('  a  \n\n   \n\n b ')).toEqual(['a', 'b']);
	});

	it('decodes common html entities', () => {
		expect(toParagraphs('a &amp; b &lt;c&gt;')).toEqual(['a & b <c>']);
	});

	it('returns an empty array for empty content', () => {
		expect(toParagraphs('')).toEqual([]);
	});
});
