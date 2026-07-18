<script lang="ts">
	// 登録済みフィードの一覧と削除(店との別れ、CONTEXT.md「フィードの削除」)。
	// 削除は破壊的(CASCADE で記事・要約・既読の事実ごと消える)なので一発では発火させず、
	// 警告様式(DESIGN_LANGUAGE §2.4: geppaku 面 + kindei 1px 枠 + アイコン + 「注意:」)の
	// 二段確認を挟む。紺紙金泥ブロックはエラー専用なので確認には使わない。
	// 実際の削除は named form action(/feeds?/delete)への native POST — 登録フォームと同じ
	// 素朴な経路(use:enhance なし)で、redirect-after-POST に乗る。
	import type { Feed } from '$lib/api/schemas';
	import { formatDate } from '$lib/format';
	import { DELETE_CANCEL, DELETE_CONFIRM_LABEL, DELETE_FEED, DELETE_FEED_WARNING } from '$lib/copy';

	let { feeds }: { feeds: Feed[] } = $props();

	// 確認は同時に1店だけ開く(迷いを増やさない)
	let confirmingId = $state<number | null>(null);
</script>

<ul class="feed-list">
	{#each feeds as f (f.id)}
		<li>
			<div class="row">
				<div class="info">
					<span class="title">{f.title || f.url}</span>
					<span class="meta">{f.url} — {formatDate(f.created_at)} に登録</span>
				</div>
				{#if confirmingId !== f.id}
					<button type="button" class="delete-trigger" onclick={() => (confirmingId = f.id)}>
						{DELETE_FEED}
					</button>
				{/if}
			</div>
			{#if confirmingId === f.id}
				<section class="confirm" aria-label={DELETE_CONFIRM_LABEL}>
					<p class="confirm-text">
						<svg aria-hidden="true" width="14" height="14" viewBox="0 0 16 16" fill="none">
							<path
								d="M8 2.2 14.6 13.4H1.4L8 2.2Z"
								stroke="currentColor"
								stroke-width="1.4"
								stroke-linejoin="round"
							/>
							<path d="M8 6.6v3" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
							<circle cx="8" cy="11.6" r="0.8" fill="currentColor" />
						</svg>
						<span>注意: {DELETE_FEED_WARNING}</span>
					</p>
					<form class="confirm-actions" method="POST" action="/feeds?/delete">
						<input type="hidden" name="id" value={f.id} />
						<button type="submit" class="confirm-delete">{DELETE_FEED}</button>
						<button type="button" class="confirm-cancel" onclick={() => (confirmingId = null)}>
							{DELETE_CANCEL}
						</button>
					</form>
				</section>
			{/if}
		</li>
	{/each}
</ul>

<style>
	.feed-list {
		list-style: none;
		margin: 24px 0 0;
		padding: 0;
	}
	.feed-list li {
		padding: 12px 0 14px;
		border-bottom: 0.5px solid var(--hatoba);
	}
	.row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 16px;
	}
	.info {
		min-width: 0;
	}
	.title {
		display: block;
		font: 500 14px/1.8 var(--font-ui);
		color: var(--kon);
	}
	.meta {
		display: block;
		margin-top: 2px;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
		overflow-wrap: anywhere;
	}
	/* 削除の入口は静かな二次操作 — 面もボーダーも持たせない(破壊の重みは確認段が引き受ける) */
	.delete-trigger {
		flex: none;
		min-height: 44px;
		padding: 0 8px;
		border: none;
		background: none;
		color: var(--konnezu);
		font: 500 12px/1 var(--font-ui);
		cursor: pointer;
		transition: color var(--dur-fast) var(--ease-calm);
	}
	.delete-trigger:hover {
		color: var(--kon);
	}
	/* 警告様式(§2.4): geppaku 面 + kindei 1px 枠 + アイコン + 「注意:」 */
	.confirm {
		margin-top: 12px;
		padding: 12px 14px;
		background: var(--geppaku);
		border: 1px solid var(--kindei);
		border-radius: var(--radius-card);
	}
	.confirm-text {
		display: flex;
		align-items: flex-start;
		gap: 8px;
		margin: 0 0 10px;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--kon);
	}
	.confirm-text svg {
		flex: none;
		margin-top: 2px;
		color: var(--kindei);
	}
	.confirm-actions {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.confirm-delete {
		min-height: 44px;
		padding: 0 14px;
		border: 1px solid var(--hatoba);
		border-radius: var(--radius-control);
		background: var(--geppaku);
		color: var(--kon);
		font: 500 12px/1 var(--font-ui);
		cursor: pointer;
	}
	.confirm-cancel {
		min-height: 44px;
		padding: 0 8px;
		border: none;
		background: none;
		color: var(--konnezu);
		font: 500 12px/1 var(--font-ui);
		cursor: pointer;
	}
	.confirm-cancel:hover {
		color: var(--kon);
	}
</style>
