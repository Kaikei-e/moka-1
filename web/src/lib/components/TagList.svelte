<script lang="ts">
	// タグchip表示(SummaryCard と同じ DESIGN_LANGUAGE §8.1: AI の声なので fujinezu 面 +
	// ゴシック、印は金泥)。enrich.Scheduler が新着記事にタグを自動付与するため、
	// マウント時に GET /articles/{id}/tags で「濃縮済みか」を純粋に確認する(LLM は
	// 呼ばない)。あれば即表示、無ければ明示ボタンにフォールバックする(SummaryCard と対称)。
	// M1 では force(やり直し)を持たない — article_tags は追記のみで削除しないため、
	// summarize の「要約をやり直す」ほど意味を持たない(grill決定)。
	//
	// SvelteKit は同一ルート内の遷移でこのコンポーネントインスタンスを再利用するため、
	// articleId の変化を検知して状態をリセットする。
	import DripIndicator from './DripIndicator.svelte';
	import { EXTRACT_TAGS, EXTRACTING_TAGS, RETRY_EXTRACT_TAGS } from '$lib/copy';

	let { articleId }: { articleId: number } = $props();

	let tags = $state<string[] | null>(null);
	let loading = $state(false);
	let error = $state<string | null>(null);
	// checked: マウント時の GET 確認が完了したか(SummaryCard.checked と同じ役割)。
	let checked = $state(false);

	$effect(() => {
		const id = articleId; // 依存の確立(記事切り替えのたびにリセットする)
		tags = null;
		loading = false;
		error = null;
		checked = false;

		void (async () => {
			try {
				const res = await fetch(`/articles/${id}/tags`);
				if (id !== articleId) return; // 応答待ちの間に記事が切り替わった
				if (res.ok) {
					const body = await res.json();
					if (id !== articleId) return;
					tags = body.tags ?? null;
				}
			} catch {
				// 確認できなくても明示ボタンにフォールバックするだけ(fail-soft)
			} finally {
				if (id === articleId) checked = true;
			}
		})();
	});

	async function extract() {
		const id = articleId;
		loading = true;
		error = null;
		try {
			const res = await fetch(`/articles/${id}/tags`, { method: 'POST' });
			const body = await res.json();
			if (id !== articleId) return;
			if (!res.ok) {
				error = body.error ?? 'タグの抽出に失敗しました。再試行してください';
				return;
			}
			tags = body.tags ?? [];
		} catch {
			if (id !== articleId) return;
			error = 'タグの抽出に失敗しました。再試行してください';
		} finally {
			if (id === articleId) loading = false;
		}
	}
</script>

{#if checked && (tags?.length || loading || error)}
	<section class="tag-list">
		{#if loading}
			<DripIndicator label={EXTRACTING_TAGS} testid="tags-drip" />
		{/if}
		{#if error}
			<p class="tags-error" role="alert">
				<span aria-hidden="true">⚠</span>
				失敗: {error}
			</p>
		{/if}
		{#if tags?.length}
			<ul class="chips">
				{#each tags as tag (tag)}
					<li class="chip">{tag}</li>
				{/each}
			</ul>
		{/if}
	</section>
{:else if checked}
	<button class="extract-button" onclick={extract}
		>{error ? RETRY_EXTRACT_TAGS : EXTRACT_TAGS}</button
	>
{/if}

<style>
	.tag-list {
		background: var(--fujinezu);
		border-radius: var(--radius-card);
		padding: 10px 14px;
	}
	.chips {
		list-style: none;
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin: 0;
		padding: 0;
	}
	.chip {
		padding: 3px 10px;
		border-radius: 999px;
		background: var(--geppaku);
		color: var(--kon);
		font: 400 12px/1.6 var(--font-ui);
	}
	.tags-error {
		margin: 0 0 8px;
		padding: 10px 12px;
		border-radius: var(--radius-card);
		background: var(--kon);
		color: var(--kindei-bright);
		font: 400 12px/1.6 var(--font-ui);
	}
	.extract-button {
		min-height: 44px;
		padding: 0 14px;
		border: 1px solid var(--hatoba);
		border-radius: var(--radius-control);
		background: var(--geppaku);
		color: var(--kon);
		font: 500 12px/1 var(--font-ui);
		cursor: pointer;
	}
</style>
