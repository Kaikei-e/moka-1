<script lang="ts">
	// 要約カード(DESIGN_LANGUAGE §8.1)。AI の声なので fujinezu 面 + ゴシック、印は金泥。
	//
	// enrich.Scheduler(常駐エージェントループ)が新着記事に要約を自動付与するようになった
	// ため、マウント時に GET /articles/{id}/summary で「濃縮済みか」を純粋に確認する
	// (LLM は呼ばない)。あれば即表示、無ければ全文取り寄せ(§5.3)と同じ明示ボタンの
	// 作法にフォールバックする(grill決定 — 自動生成済みのものをボタンの裏に隠さない)。
	// moka-core 側は冪等なので、既に生成済みなら再生成せず保存済みの要約をそのまま返す。
	//
	// SvelteKit は同一ルート内の遷移でこのコンポーネントインスタンスを再利用するため、
	// articleId の変化を検知して状態をリセットする(記事ごとに確認からやり直す)。
	import DripIndicator from './DripIndicator.svelte';
	import { REGENERATE_SUMMARY, RETRY_SUMMARIZE, SUMMARIZE, SUMMARIZING } from '$lib/copy';

	let { articleId }: { articleId: number } = $props();

	let text = $state<string | null>(null);
	let loading = $state(false);
	// ストリーミング(逐次表示)中は面に min-height を与えてレイアウトの跳ねを抑える。
	// loading(最初のトークンまで)と違い、ストリームが閉じるまで立ちっぱなしのフラグ
	let streaming = $state(false);
	let error = $state<string | null>(null);
	// checked: マウント時の GET 確認が完了したか。完了するまではボタンもテキストも
	// 出さない(まだ濃縮済みかどうか分からない状態で「要約する」ボタンを一瞬見せない)。
	let checked = $state(false);
	// 直前の試行が通常生成か「やり直し」かを覚えておく — 失敗後の再試行ボタンは
	// 同じモードで再送する(やり直しの失敗を「要約する」で再試行すると、moka-core が
	// 保存済みの古い要約をそのまま返すだけで何も変わらない)。
	let lastForce = $state(false);

	$effect(() => {
		const id = articleId; // 依存の確立(記事切り替えのたびにリセットする)
		text = null;
		loading = false;
		streaming = false;
		error = null;
		checked = false;
		lastForce = false;

		void (async () => {
			try {
				const res = await fetch(`/articles/${id}/summary`);
				if (id !== articleId) return; // 応答待ちの間に記事が切り替わった
				if (res.ok) {
					const body = await res.json();
					if (id !== articleId) return;
					text = body.summary?.text ?? null;
				}
			} catch {
				// 確認できなくても明示ボタンにフォールバックするだけ(fail-soft)
			} finally {
				if (id === articleId) checked = true;
			}
		})();
	});

	// SSE の1イベント("event: foo\ndata: {...}"、\n\n区切り済み)を状態へ反映する。
	// delta: 到着順に本文を連結して逐次表示。done: 保存済みの最終テキストで確定。
	// error: 部分テキストは破棄して(moka-core 側も保存しない)失敗表示のみ残す。
	function applySSEEvent(raw: string) {
		let eventName = 'message';
		let data = '';
		for (const line of raw.split('\n')) {
			if (line.startsWith('event: ')) eventName = line.slice('event: '.length);
			else if (line.startsWith('data: ')) data = line.slice('data: '.length);
		}
		if (!data) return;
		const payload = JSON.parse(data);
		if (eventName === 'delta') {
			text = (text ?? '') + payload.text;
			loading = false;
		} else if (eventName === 'done') {
			text = payload.summary?.text ?? text ?? '';
			loading = false;
		} else if (eventName === 'error') {
			error = payload.error ?? '要約に失敗しました。再試行してください';
			text = null;
			loading = false;
		}
	}

	async function summarize(force = false) {
		// ストリーム中に記事が切り替わったら(コンポーネントは再利用される)残りは捨てて読むのをやめる
		const id = articleId;
		lastForce = force;
		loading = true;
		streaming = true;
		error = null;
		text = null;
		try {
			const url = `/articles/${id}/summary/stream${force ? '?force=true' : ''}`;
			const res = await fetch(url, { method: 'POST' });
			if (id !== articleId) return;
			if (!res.ok || !res.body) {
				const body = await res.json().catch(() => ({}));
				if (id !== articleId) return;
				error = body.error ?? '要約に失敗しました。再試行してください';
				return;
			}

			const reader = res.body.getReader();
			const decoder = new TextDecoder();
			let buffer = '';
			for (;;) {
				const { done, value } = await reader.read();
				if (id !== articleId) {
					void reader.cancel();
					return;
				}
				if (done) break;
				buffer += decoder.decode(value, { stream: true });
				let sep = buffer.indexOf('\n\n');
				while (sep !== -1) {
					applySSEEvent(buffer.slice(0, sep));
					buffer = buffer.slice(sep + 2);
					sep = buffer.indexOf('\n\n');
				}
			}
		} catch {
			if (id !== articleId) return;
			error = '要約に失敗しました。再試行してください';
			text = null;
		} finally {
			if (id === articleId) {
				loading = false;
				streaming = false;
			}
		}
	}
</script>

<section class="summary-card" data-testid="summary-card" data-streaming={streaming || undefined}>
	<h2 class="card-label">
		<svg aria-hidden="true" width="14" height="14" viewBox="0 0 16 16" fill="none">
			<path
				d="M3 6h8v3.5A3.5 3.5 0 0 1 7.5 13h-1A3.5 3.5 0 0 1 3 9.5V6Z"
				stroke="currentColor"
				stroke-width="1.4"
			/>
			<path d="M11 7h1a1.6 1.6 0 0 1 0 3.2h-1" stroke="currentColor" stroke-width="1.4" />
		</svg>
		moka による要約
	</h2>
	{#if checked && text === null && !loading}
		<button class="summarize-button" onclick={() => summarize(lastForce)}
			>{error ? RETRY_SUMMARIZE : SUMMARIZE}</button
		>
	{/if}
	{#if loading}
		<DripIndicator label={SUMMARIZING} testid="summary-drip" />
	{/if}
	{#if error}
		<p class="summary-error" role="alert">
			<span aria-hidden="true">⚠</span>
			失敗: {error}
		</p>
	{/if}
	{#if text}
		<p class="summary-text" data-testid="summary-text">{text}</p>
		<button class="regenerate-button" onclick={() => summarize(true)}>{REGENERATE_SUMMARY}</button>
	{/if}
</section>

<style>
	.summary-card {
		/* ボーダーなし — 面の濃淡差のみで区切る(§8.1) */
		background: var(--fujinezu);
		border-radius: var(--radius-card);
		padding: 14px;
	}
	/* ストリーミング中は数行ぶんの高さを先に確保し、逐次表示で本文が跳ねないようにする(§8.1) */
	.summary-card[data-streaming] {
		min-height: 128px;
	}
	.card-label {
		display: flex;
		align-items: center;
		gap: 6px;
		margin: 0 0 10px;
		font: 500 11px/1.5 var(--font-ui);
		color: var(--kindei); /* moka の印 = 金泥 */
	}
	.summary-text {
		margin: 0;
		font: 400 13px/1.8 var(--font-ui);
		color: var(--kon);
	}
	/* エラー = 紺紙金泥ブロック(DESIGN_LANGUAGE §2.4) */
	.summary-error {
		margin: 0 0 10px;
		padding: 12px 14px;
		border-radius: var(--radius-card);
		background: var(--kon);
		color: var(--kindei-bright);
		font: 400 12px/1.6 var(--font-ui);
	}
	.summarize-button {
		min-height: 44px;
		padding: 0 14px;
		border: 1px solid var(--hatoba);
		border-radius: var(--radius-control);
		background: var(--geppaku);
		color: var(--kon);
		font: 500 12px/1 var(--font-ui);
		cursor: pointer;
	}
	/* 記事が主役・AI は給仕(DESIGN_LANGUAGE 冒頭) — やり直しは控えめな二次操作にする。
	   枠線・背景は持たせず、タップターゲットの高さだけ .summarize-button と揃える */
	.regenerate-button {
		display: block;
		margin-top: 10px;
		min-height: 44px;
		padding: 0;
		border: none;
		background: none;
		color: var(--kon);
		font: 500 11px/1.5 var(--font-ui);
		text-align: left;
		cursor: pointer;
	}
</style>
