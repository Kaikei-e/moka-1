// SSE(text/event-stream)レスポンスの検証ヘルパ。
//
// moka-core の SSE エンドポイント(/qa, /summary/stream)は最後のイベントを書いたら
// ストリームを閉じるので、APIRequestContext の `response.text()` で全体をバッファできる
// (Hurl の `body contains "event: done"` と同じ観測モデル)。イベント「順序」の契約は
// 生文字列の indexOf ではなく、パース済みイベント名の配列で表明する。
//
// 書式は core/internal/httpapi の writeEvent と 1:1: `event: <name>\ndata: <json>\n\n`

import { expect, type APIResponse } from '@playwright/test';

export type SseEvent = {
	name: string;
	/** data 行の生文字列(複数行 data は改行で連結)。 */
	data: string;
};

/** 生ボディを SSE イベント列にパースする。data を持たないコメント行等は無視する。 */
export function parseSse(body: string): SseEvent[] {
	const events: SseEvent[] = [];
	for (const block of body.split('\n\n')) {
		let name = '';
		const dataLines: string[] = [];
		for (const line of block.split('\n')) {
			if (line.startsWith('event:')) name = line.slice('event:'.length).trim();
			else if (line.startsWith('data:')) dataLines.push(line.slice('data:'.length).trim());
		}
		if (name !== '') events.push({ name, data: dataLines.join('\n') });
	}
	return events;
}

/** イベント名だけを出現順に返す(順序契約のアサーション用)。 */
export function sseEventNames(body: string): string[] {
	return parseSse(body).map((e) => e.name);
}

/** 指定名のイベントだけを返す。 */
export function sseEventsNamed(body: string, name: string): SseEvent[] {
	return parseSse(body).filter((e) => e.name === name);
}

/** 指定名の最初のイベントの data を JSON としてパースする。無ければ undefined。 */
export function sseData<T>(body: string, name: string): T | undefined {
	const first = sseEventsNamed(body, name)[0];
	if (first === undefined) return undefined;
	return JSON.parse(first.data) as T;
}

/** delta イベントの text を連結する(ストリーミング要約 / 回答の本文)。 */
export function sseDeltaText(body: string): string {
	return sseEventsNamed(body, 'delta')
		.map((e) => (JSON.parse(e.data) as { text?: string }).text ?? '')
		.join('');
}

/**
 * Hurl の `header "Content-Type" == "text/event-stream"` 相当。
 * moka-core は charset を付けずにこの値そのものを設定する
 * (`core/internal/httpapi/{qa,summarize}.go`)ので完全一致で見る。
 */
export function expectEventStream(response: APIResponse): void {
	expect(response.headers()['content-type'], 'Content-Type が text/event-stream であること').toBe(
		'text/event-stream'
	);
}

/**
 * Hurl の `body not contains "event: error"` 相当。パース済みイベント名と生ボディの
 * 両方で否定する — パーサの取りこぼしで否定が空振りすることを防ぐ。
 */
export function expectNoSseError(body: string): void {
	expect(sseEventNames(body), 'error イベントを含まないこと').not.toContain('error');
	expect(body, 'body が "event: error" を含まないこと').not.toContain('event: error');
}
