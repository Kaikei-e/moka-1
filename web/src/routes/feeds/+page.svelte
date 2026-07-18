<script lang="ts">
	// フィード管理: 登録・削除と登録済み一覧(CONTEXT.md「フィード管理」)
	import { page } from '$app/state';
	import FeedList from '$lib/components/FeedList.svelte';
	import FeedRegisterForm from '$lib/components/FeedRegisterForm.svelte';
	import { DELETED, EMPTY_FEEDS, REGISTERED } from '$lib/copy';

	let { data, form } = $props();

	const registered = $derived(page.url.searchParams.get('registered'));
	const deleted = $derived(page.url.searchParams.get('deleted') !== null);
	// 失敗データは scope で振り分ける(登録エラーはフォーム直下、削除エラーは一覧の上)
	const registerError = $derived(form?.scope === 'register' ? form.message : null);
	const deleteError = $derived(form?.scope === 'delete' ? form.message : null);
</script>

<svelte:head>
	<title>フィード管理 — moka-1</title>
</svelte:head>

<section class="feeds-page">
	<h1>フィード管理</h1>

	<FeedRegisterForm errorMessage={registerError} />

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

	{#if deleted}
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
			{DELETED}
		</p>
	{/if}

	{#if deleteError}
		<!-- エラーは紺紙金泥ブロック: kon 地 + kindei-bright + アイコン + 「失敗:」(§2.4) -->
		<p class="delete-error" role="alert">
			<svg aria-hidden="true" width="14" height="14" viewBox="0 0 16 16" fill="none">
				<circle cx="8" cy="8" r="6.5" stroke="currentColor" stroke-width="1.4" />
				<path d="M8 4.8v4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
				<circle cx="8" cy="11.2" r="0.9" fill="currentColor" />
			</svg>
			失敗: {deleteError}
		</p>
	{/if}

	{#if data.feedsUnavailable}
		<p class="note">フィードを読み込めませんでした。再読み込みしてください</p>
	{:else if data.feeds.length === 0}
		<p class="note">{EMPTY_FEEDS}</p>
	{:else}
		<FeedList feeds={data.feeds} />
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
	.delete-error {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 16px 0 0;
		padding: 10px 14px;
		background: var(--kon);
		border-radius: var(--radius-card);
		font: 400 12px/1.6 var(--font-ui);
		color: var(--kindei-bright); /* kon 地の上のみ許可(§2.2) */
	}
	.delete-error svg {
		flex: none;
	}
	.note {
		margin: 24px 0 0;
		font: 400 14px/1.8 var(--font-ui);
		color: var(--konnezu);
	}
</style>
