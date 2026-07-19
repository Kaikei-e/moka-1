<script lang="ts">
	// 問い返し(訊く、DESIGN_LANGUAGE §8.2)。質問は kumoi 地のまま hatoba の上罫線で区切り、
	// 回答はチャット吹き出しでなく fujinezu 面のフラットなブロックとして積む(給仕は騒がない)。
	// BFF(/articles/[id]/qa)が中継する moka-core の SSE(sources / delta / done / error)を
	// SummaryCard と同じ作法で逐次読み、出典は回答の下に小さく列挙する。
	//
	// SvelteKit は同一ルート内の遷移でこのコンポーネントインスタンスを再利用するため、
	// articleId の変化を検知して質問スタックと下書きをリセットする(SummaryCard と同じ作法)
	import { resolve } from '$app/paths';
	import DripIndicator from './DripIndicator.svelte';
	import { ANSWERING, ASK_FAILED, SOURCES_LABEL } from '$lib/copy';
	import { readSSEStream, type SSEFrame } from '$lib/sse';

	type QASource = { id: number; title: string; url: string };
	type QAEntry = {
		question: string;
		answer: string;
		sources: QASource[];
		status: 'pending' | 'streaming' | 'done' | 'failed';
		error: string | null;
	};

	let { articleId }: { articleId: number } = $props();

	let draft = $state('');
	let entries = $state<QAEntry[]>([]);

	$effect(() => {
		const id = articleId; // 依存の確立(記事切り替えのたびにリセットする)
		void id;
		draft = '';
		entries = [];
	});

	// SSE の1フレームを該当エントリへ反映する。sources: 出典の記事(先に届く)。
	// delta: 到着順に回答を連結して逐次表示。done: 確定。
	// error: moka-core の message は技術文言("llm unavailable" 等)なので読者には見せず、
	// 給仕の静かな文言に置き換える(フェイルソフト — 技術用語を出さない)。
	function applySSEFrame(entry: QAEntry, frame: SSEFrame) {
		if (!frame.data) return;
		const payload = JSON.parse(frame.data);
		if (frame.event === 'sources') {
			entry.sources = payload.articles ?? [];
		} else if (frame.event === 'delta') {
			entry.answer += payload.text;
			entry.status = 'streaming';
		} else if (frame.event === 'done') {
			entry.status = 'done';
		} else if (frame.event === 'error') {
			entry.status = 'failed';
			entry.error = ASK_FAILED;
		}
	}

	async function ask(e: SubmitEvent) {
		e.preventDefault();
		const question = draft.trim();
		if (!question) return;
		draft = '';

		// ストリーム中に記事が切り替わったら(コンポーネントは再利用される)残りは捨てて読むのをやめる
		const id = articleId;
		entries.push({ question, answer: '', sources: [], status: 'pending', error: null });
		// push した素のオブジェクトでなく、$state プロキシ越しの参照を掴んで変異させる
		// (素の参照への変異はリアクティビティに乗らない)
		const entry = entries[entries.length - 1];
		if (!entry) return;
		try {
			const res = await fetch(`/articles/${id}/qa`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ question })
			});
			if (id !== articleId) return;
			if (!res.ok || !res.body) {
				const body = await res.json().catch(() => ({}));
				if (id !== articleId) return;
				entry.status = 'failed';
				entry.error = body.error ?? ASK_FAILED;
				return;
			}

			const completed = await readSSEStream(
				res.body,
				(frame) => applySSEFrame(entry, frame),
				() => id === articleId
			);
			if (!completed) return;
			// done イベント無しでストリームが閉じた場合も、届いた分は残して確定させる
			if (entry.status === 'streaming') entry.status = 'done';
		} catch {
			if (id !== articleId) return;
			entry.status = 'failed';
			entry.error = ASK_FAILED;
		}
	}
</script>

<section class="qa-zone">
	{#each entries as entry, i (i)}
		<div class="qa">
			<p class="question">{entry.question}</p>
			{#if entry.status === 'failed'}
				<!-- エラー = 紺紙金泥ブロック(§2.4)。文言は給仕の声 — 技術用語を出さない -->
				<p class="qa-error" role="alert">
					<span aria-hidden="true">⚠</span>
					失敗: {entry.error}
				</p>
			{:else}
				<!-- 回答は AI 生成物 — fujinezu 面のフラットなブロック(§8.2)。
				     逐次表示中は min-height を確保してレイアウトの跳ねを抑える(§8.1 と同じ) -->
				<div class="answer" data-streaming={entry.status === 'streaming' || undefined}>
					{#if entry.status === 'pending'}
						<DripIndicator label={ANSWERING} testid="qa-drip" />
					{:else}
						<p class="answer-text" data-testid="qa-answer">{entry.answer}</p>
						{#if entry.sources.length > 0}
							<p class="sources">
								<span class="sources-label">{SOURCES_LABEL}</span>
								{#each entry.sources as source (source.id)}
									<a href={resolve('/(app)/articles/[id]', { id: String(source.id) })}>
										{source.title}
									</a>
								{/each}
							</p>
						{/if}
					{/if}
				</div>
			{/if}
		</div>
	{/each}

	<form class="ask" onsubmit={ask}>
		<input
			type="text"
			placeholder="この記事について訊く…"
			aria-label="この記事について訊く"
			bind:value={draft}
		/>
		<button type="submit" aria-label="訊く">
			<svg aria-hidden="true" width="16" height="16" viewBox="0 0 16 16" fill="none">
				<path
					d="M2.5 8h10M9 4.5 13 8l-4 3.5"
					stroke="currentColor"
					stroke-width="1.5"
					stroke-linecap="round"
					stroke-linejoin="round"
				/>
			</svg>
		</button>
	</form>
</section>

<style>
	.qa {
		border-top: 1px solid var(--hatoba);
		padding-top: 12px;
		margin-top: 16px;
	}
	.question {
		margin: 0 0 8px;
		font: 400 14px/1.8 var(--font-ui);
		color: var(--kon);
	}
	.answer {
		/* フラットなブロック — 面の濃淡差のみで区切る */
		background: var(--fujinezu);
		border-radius: var(--radius-card);
		padding: 12px;
	}
	/* 逐次表示の間じゅう数行ぶんの高さを確保し、トークン到着で本文が跳ねないようにする */
	.answer[data-streaming] {
		min-height: 96px;
	}
	.answer-text {
		margin: 0;
		font: 400 13px/1.8 var(--font-ui);
		color: var(--kon);
	}
	/* 出典 — 回答の下に小さく。moka の印ではないので金泥は使わない */
	.sources {
		margin: 10px 0 0;
		display: flex;
		flex-wrap: wrap;
		gap: 4px 12px;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
	}
	.sources a {
		color: var(--ruri);
		text-decoration: underline;
		text-underline-offset: 2px;
	}
	/* エラー = 紺紙金泥ブロック(DESIGN_LANGUAGE §2.4) */
	.qa-error {
		margin: 0;
		padding: 12px 14px;
		border-radius: var(--radius-card);
		background: var(--kon);
		color: var(--kindei-bright);
		font: 400 12px/1.6 var(--font-ui);
	}
	.ask {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-top: 16px;
		background: var(--geppaku);
		border: 0.5px solid var(--hatoba);
		border-radius: 999px; /* pill(§4.2) */
		padding: 6px 6px 6px 16px;
	}
	.ask input {
		flex: 1;
		min-width: 0;
		border: none;
		background: transparent;
		font: 400 14px/1.8 var(--font-ui);
		color: var(--kon);
	}
	.ask input:focus-visible {
		outline: none;
		box-shadow: none; /* リングは pill 全体に付ける */
	}
	.ask:focus-within {
		box-shadow:
			0 0 0 2px var(--kumoi),
			0 0 0 4px var(--ruri);
	}
	.ask button {
		flex: none;
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		border: none;
		border-radius: 50%;
		background: transparent;
		color: var(--ruri);
		cursor: pointer;
	}
	.ask button:hover {
		color: var(--ruri-deep);
	}

	/* モバイル(< 900px): 入力バーは読書カラムの下部に沈む(§4.3、safe-area 考慮)。
	   質問スタックの末尾に置かれた form が sticky で液面のように留まる */
	@media (max-width: 899.98px) {
		.ask {
			position: sticky;
			bottom: 0;
			margin-bottom: 0;
		}
		.qa-zone {
			padding-bottom: env(safe-area-inset-bottom);
		}
	}
</style>
