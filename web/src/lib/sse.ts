// SSE フレーミングの純ロジック(SummaryCard / AskBar の逐次表示が共用)。
// コンポーネントに埋めず lib に置くのは、純ロジックを node 環境の vitest で守るため。
// フレームは "event: name\ndata: {...}" の1ブロック(\n\n 区切り済みで渡される)。

export type SSEFrame = { event: string; data: string };

export function parseSSEFrame(raw: string): SSEFrame {
	let event = 'message';
	let data = '';
	for (const line of raw.split('\n')) {
		if (line.startsWith('event: ')) event = line.slice('event: '.length);
		else if (line.startsWith('data: ')) data = line.slice('data: '.length);
	}
	return { event, data };
}

// body を \n\n 区切りで読み進め、フレームごとに onFrame を呼ぶ。keepReading が false を
// 返したらストリームをキャンセルして false を返す(記事切り替えで残りの応答を捨てる作法)。
// 最後まで読み切ったら true。
export async function readSSEStream(
	body: ReadableStream<Uint8Array>,
	onFrame: (frame: SSEFrame) => void,
	keepReading: () => boolean = () => true
): Promise<boolean> {
	const reader = body.getReader();
	const decoder = new TextDecoder();
	let buffer = '';
	for (;;) {
		const { done, value } = await reader.read();
		if (!keepReading()) {
			void reader.cancel();
			return false;
		}
		if (done) return true;
		buffer += decoder.decode(value, { stream: true });
		let sep = buffer.indexOf('\n\n');
		while (sep !== -1) {
			onFrame(parseSSEFrame(buffer.slice(0, sep)));
			buffer = buffer.slice(sep + 2);
			sep = buffer.indexOf('\n\n');
		}
	}
}
