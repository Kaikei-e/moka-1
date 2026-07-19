<script lang="ts">
	// サイドバーの検索(ハイブリッド検索の入口)。検索は道具 — 記事が主役なので静かに:
	// 入力をデバウンスして BFF(/search)へ、結果はリスト行と同じ手がかり(タイトル + メタ)で
	// 並べる。score は順位に折り込まれているので数字は見せない。空クエリで active を下ろし、
	// 親(レイアウト)が通常一覧を戻す。待ちはドリップ + 工程コピーのみ(装飾アニメーション無し)。
	import { resolve } from '$app/paths';
	import type { SearchResult } from '$lib/api/schemas';
	import { toArticleListItem } from '$lib/article-list-item';
	import {
		SEARCHING,
		SEARCH_EMPTY,
		SEARCH_FAILED,
		SEARCH_LABEL,
		SEARCH_PLACEHOLDER
	} from '$lib/copy';
	import DripIndicator from './DripIndicator.svelte';

	let {
		currentId = null,
		active = $bindable(false)
	}: { currentId?: number | null; active?: boolean } = $props();

	const DEBOUNCE_MS = 300; // dur-calm と同じ間 — 打鍵のたびに問い合わせない

	let query = $state('');
	// 大きく置換されるデータは $state.raw(bp-svelte §5)。null = まだ一度も検索していない
	let results = $state.raw<SearchResult[] | null>(null);
	let searching = $state(false);
	let failed = $state(false);
	// クエリ変更のたびに進む世代番号。飛行中の応答が古い世代なら丸ごと捨てる
	// (リアクティブに描画へは使わないので素の変数でよい)
	let generation = 0;

	$effect(() => {
		const q = query.trim();
		generation += 1;
		const gen = generation;
		if (q === '') {
			active = false;
			results = null;
			searching = false;
			failed = false;
			return;
		}
		active = true;
		const timer = setTimeout(() => void search(q, gen), DEBOUNCE_MS);
		return () => clearTimeout(timer);
	});

	async function search(q: string, gen: number) {
		searching = true;
		failed = false;
		try {
			const res = await fetch(`/search?q=${encodeURIComponent(q)}`);
			if (gen !== generation) return;
			if (!res.ok) throw new Error(`search: ${res.status}`);
			const body = await res.json();
			if (gen !== generation) return;
			results = body.items;
		} catch {
			if (gen !== generation) return;
			failed = true;
			results = null;
		} finally {
			if (gen === generation) searching = false;
		}
	}

	const listItems = $derived((results ?? []).map((r) => toArticleListItem(r)));
</script>

<form class="search" role="search" onsubmit={(e) => e.preventDefault()}>
	<input
		type="search"
		placeholder={SEARCH_PLACEHOLDER}
		aria-label={SEARCH_LABEL}
		bind:value={query}
	/>
</form>

{#if active}
	<div class="search-results">
		{#if searching}
			<div class="search-wait">
				<DripIndicator label={SEARCHING} testid="search-drip" />
			</div>
		{/if}
		{#if failed}
			<p class="search-note" role="alert">{SEARCH_FAILED}</p>
		{:else if results !== null && results.length === 0 && !searching}
			<p class="search-note">{SEARCH_EMPTY}</p>
		{:else if listItems.length > 0}
			<nav aria-label="検索結果">
				<ul class="results">
					{#each listItems as item (item.id)}
						<li>
							<a
								href={resolve('/(app)/articles/[id]', { id: String(item.id) })}
								aria-current={item.id === currentId ? 'page' : undefined}
								data-read={item.read ? 'true' : undefined}
							>
								<span class="title">{item.title}</span>
								{#if item.meta}
									<span class="meta">{item.meta}</span>
								{/if}
							</a>
						</li>
					{/each}
				</ul>
			</nav>
		{/if}
	</div>
{/if}

<style>
	.search {
		padding: 12px 16px;
		border-bottom: 0.5px solid var(--hatoba);
	}
	.search input {
		width: 100%;
		border: 1px solid var(--hatoba);
		border-radius: var(--radius-control);
		background: var(--geppaku);
		padding: 10px 12px;
		font: 400 14px/1.8 var(--font-ui);
		color: var(--kon);
	}
	.search-wait {
		padding: 12px 16px;
	}
	.search-note {
		margin: 0;
		padding: 12px 16px;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
	}

	/* 結果行は記事リスト行と同じ手がかり(§8.6): タイトル + メタ、hairline 区切りのみ */
	.results {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.results a {
		display: block;
		padding: 12px 16px;
		text-decoration: none;
		color: var(--kon);
		border-bottom: 0.5px solid var(--hatoba);
		transition: background-color var(--dur-fast) var(--ease-calm);
	}
	.results a:hover {
		background: var(--hatoba);
	}
	.results a[aria-current='page'] {
		background: var(--ruri-tint);
	}
	.title {
		display: block;
		font: 500 14px/1.7 var(--font-ui);
		color: var(--kon);
	}
	.results a[data-read='true'] .title {
		font-weight: 400;
		color: var(--konnezu);
	}
	.meta {
		display: block;
		margin-top: 2px;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
	}
</style>
