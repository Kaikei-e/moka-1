import { describe, expect, it } from 'vitest';
import { parseSSEFrame, readSSEStream, type SSEFrame } from './sse';

function streamOf(chunks: string[]): ReadableStream<Uint8Array> {
	const encoder = new TextEncoder();
	return new ReadableStream({
		start(controller) {
			for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
			controller.close();
		}
	});
}

describe('parseSSEFrame', () => {
	it('reads the event name and the data line', () => {
		expect(parseSSEFrame('event: delta\ndata: {"text":"x"}')).toEqual({
			event: 'delta',
			data: '{"text":"x"}'
		});
	});

	it('defaults the event name to "message" when absent', () => {
		expect(parseSSEFrame('data: {"a":1}')).toEqual({ event: 'message', data: '{"a":1}' });
	});
});

describe('readSSEStream', () => {
	it('emits one frame per \\n\\n block, even across chunk boundaries', async () => {
		const frames: SSEFrame[] = [];
		const completed = await readSSEStream(
			streamOf(['event: delta\ndata: {"text":"a"}\n\nevent: del', 'ta\ndata: {"text":"b"}\n\n']),
			(f) => frames.push(f)
		);

		expect(completed).toBe(true);
		expect(frames).toEqual([
			{ event: 'delta', data: '{"text":"a"}' },
			{ event: 'delta', data: '{"text":"b"}' }
		]);
	});

	it('cancels quietly and reports false when keepReading turns off (記事切り替え)', async () => {
		const frames: SSEFrame[] = [];
		let keep = true;
		const completed = await readSSEStream(
			streamOf(['event: delta\ndata: {"text":"a"}\n\n', 'event: done\ndata: {}\n\n']),
			(f) => {
				frames.push(f);
				keep = false; // 1 フレーム目を読んだところで購読者がいなくなった
			},
			() => keep
		);

		expect(completed).toBe(false);
		expect(frames).toEqual([{ event: 'delta', data: '{"text":"a"}' }]);
	});
});
