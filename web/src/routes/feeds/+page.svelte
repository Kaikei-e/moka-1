<script lang="ts">
	// フィード管理: 登録と登録済み一覧(CONTEXT.md「フィード管理」)
	import { page } from '$app/state';
	import FeedRegisterForm from '$lib/components/FeedRegisterForm.svelte';
	import { EMPTY_FEEDS, REGISTERED } from '$lib/copy';
	import { formatDate } from '$lib/format';

	let { data, form } = $props();

	const registered = $derived(page.url.searchParams.get('registered'));
</script>

<svelte:head>
	<title>フィード管理 — moka-1</title>
</svelte:head>

<section class="feeds-page">
	<h1>フィード管理</h1>

	<FeedRegisterForm errorMessage={form?.message ?? null} />

	{#if registered !== null}
		<!-- 成功は fujinezu 面 + チェックアイコン + 完了文言(§2.4) -->
		<p class="toast" role="status">
			<svg aria-hidden="true" width="14" height="14" viewBox="0 0 16 16" fill="none">
				<path
					d="m3.5 8.5 3 3 6-7"
					stroke="currentColor"
					stroke-width="1.6"
					stroke-linecap="round"
					stroke-linejoin="round"
				/>
			</svg>
			{REGISTERED}{#if Number(registered) > 0}。{registered} 件の記事が届きました{/if}
		</p>
	{/if}

	{#if data.feedsUnavailable}
		<p class="note">フィードを読み込めませんでした。再読み込みしてください</p>
	{:else if data.feeds.length === 0}
		<p class="note">{EMPTY_FEEDS}</p>
	{:else}
		<ul class="feed-list">
			{#each data.feeds as f (f.id)}
				<li>
					<span class="title">{f.title || f.url}</span>
					<span class="meta">{f.url} — {formatDate(f.created_at)} に登録</span>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
	.feeds-page {
		max-width: var(--measure-ja);
		margin: 0 auto;
	}
	h1 {
		margin: 0 0 24px;
		font: 500 18px/1.7 var(--font-ui);
	}
	.toast {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 16px 0 0;
		padding: 10px 14px;
		background: var(--fujinezu);
		border-radius: var(--radius-card);
		font: 400 13px/1.8 var(--font-ui);
		color: var(--kon);
	}
	.toast svg {
		flex: none;
	}
	.note {
		margin: 24px 0 0;
		font: 400 14px/1.8 var(--font-ui);
		color: var(--konnezu);
	}
	.feed-list {
		list-style: none;
		margin: 24px 0 0;
		padding: 0;
	}
	.feed-list li {
		padding: 12px 0;
		border-bottom: 0.5px solid var(--hatoba);
	}
	.title {
		display: block;
		font: 500 14px/1.8 var(--font-ui);
	}
	.meta {
		display: block;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
		overflow-wrap: anywhere;
	}
</style>
