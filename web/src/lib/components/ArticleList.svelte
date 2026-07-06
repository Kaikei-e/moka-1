<script lang="ts">
	// 無限スクロール: 末尾のセンチネルが可視化したら articles/+server.ts から次ページを取る。
	// SSR props(articles/nextCursor)が変わったら(例: 新規フィード登録後の再読み込み)、
	// 積み上げた追加ページは破棄して1ページ目からやり直す — 重複や矛盾を避ける単純な方針
	import { resolve } from '$app/paths';
	import type { Article } from '$lib/api/schemas';
	import { EMPTY_ARTICLES, LOADING_MORE, LOAD_MORE_FAILED, RETRY_LOAD_MORE } from '$lib/copy';
	import { formatDate } from '$lib/format';
	import DripIndicator from './DripIndicator.svelte';

	let {
		articles,
		nextCursor = null,
		currentId = null
	}: { articles: Article[]; nextCursor?: string | null; currentId?: number | null } = $props();

	// items/cursor はここでは初期値としてのみ props を読む(SSR の初期表示と一致させるため)。
	// props が後から変わった場合の再同期は下の $effect が担う — 意図的な一回きりの参照
	// svelte-ignore state_referenced_locally
	let items = $state<Article[]>(articles);
	// svelte-ignore state_referenced_locally
	let cursor = $state<string | null>(nextCursor);
	let loading = $state(false);
	let failed = $state(false);
	let sentinel = $state<HTMLElement | null>(null);
	// props リセットのたびに進む世代番号。飛行中の loadMore が古い世代なら結果を丸ごと捨てる
	// (リアクティブに描画へは使わないので素の変数でよい)
	let generation = 0;

	$effect(() => {
		items = articles;
		cursor = nextCursor;
		failed = false;
		loading = false;
		generation += 1;
	});

	async function loadMore() {
		if (loading || cursor === null) return;
		const gen = generation;
		loading = true;
		failed = false;
		try {
			const res = await fetch(`/articles?cursor=${encodeURIComponent(cursor)}`);
			if (!res.ok) throw new Error(`load more articles: ${res.status}`);
			const body = await res.json();
			if (gen !== generation) return;
			items = [...items, ...body.articles];
			cursor = body.next_cursor;
		} catch {
			if (gen !== generation) return;
			failed = true;
		} finally {
			if (gen === generation) loading = false;
		}
	}

	$effect(() => {
		if (!sentinel) return;
		const el = sentinel;
		const observer = new IntersectionObserver((entries) => {
			if (entries[0]?.isIntersecting) void loadMore();
		});
		observer.observe(el);
		return () => observer.disconnect();
	});
</script>

{#if items.length === 0}
	<p class="empty">{EMPTY_ARTICLES}</p>
{:else}
	<nav aria-label="記事リスト">
		<ul class="articles">
			{#each items as a (a.id)}
				<li>
					<a
						href={resolve('/articles/[id]', { id: String(a.id) })}
						aria-current={a.id === currentId ? 'page' : undefined}
					>
						<span class="title">{a.title}</span>
						{#if a.published_at}
							<span class="meta">{formatDate(a.published_at)}</span>
						{/if}
					</a>
				</li>
			{/each}
		</ul>
	</nav>
	{#if cursor !== null}
		<div
			class="sentinel"
			bind:this={sentinel}
			data-testid="article-list-sentinel"
			aria-hidden="true"
		></div>
	{/if}
	{#if loading}
		<div class="loading-more">
			<DripIndicator label={LOADING_MORE} />
		</div>
	{/if}
	{#if failed}
		<div class="load-more-failed">
			<p role="alert">{LOAD_MORE_FAILED}</p>
			<button type="button" onclick={loadMore}>{RETRY_LOAD_MORE}</button>
		</div>
	{/if}
{/if}

<style>
	.empty {
		margin: 0;
		padding: 12px 16px;
		font: 400 14px/1.8 var(--font-ui);
		color: var(--konnezu);
	}
	.articles {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.articles a {
		display: block;
		padding: 10px 16px;
		text-decoration: none;
		color: var(--kon);
		border-bottom: 0.5px solid var(--hatoba);
		transition: background-color var(--dur-fast) var(--ease-calm);
	}
	.articles a:hover {
		background: var(--hatoba); /* 選択面(§2.1)。サイドバー地 geppaku と濃淡差を付ける */
	}
	.articles a[aria-current='page'] {
		background: var(--ruri-tint);
	}
	.title {
		display: block;
		font: 400 14px/1.7 var(--font-article);
	}
	.meta {
		display: block;
		margin-top: 2px;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
	}
	.sentinel {
		height: 1px;
	}
	.loading-more {
		padding: 12px 16px;
	}
	.load-more-failed {
		padding: 12px 16px;
	}
	.load-more-failed p {
		margin: 0 0 8px;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
	}
	.load-more-failed button {
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
