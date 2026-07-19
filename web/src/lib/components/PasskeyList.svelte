<script lang="ts">
	// 登録済みパスキーの一覧と削除(/account、ADR00021)。削除は破壊的(次回からこの
	// パスキーではログインできなくなる)なので一発では発火させず、警告様式(DESIGN_LANGUAGE
	// §2.4: geppaku 面 + kindei 1px 枠 + アイコン + 「注意:」)の二段確認を挟む(FeedList と
	// 同じ流儀、ADR00019)。最後の1本を消すときは追加の警告(誰でも再登録できる状態になる旨)。
	// 実際の削除は named form action(/account?/delete)への native POST。
	import type { Passkey } from '$lib/api/schemas';
	import { formatDate } from '$lib/format';
	import {
		DELETE_CANCEL,
		DELETE_CONFIRM_LABEL,
		DELETE_LAST_PASSKEY_WARNING,
		DELETE_PASSKEY,
		DELETE_PASSKEY_WARNING,
		PASSKEY_NEVER_USED
	} from '$lib/copy';

	let { passkeys }: { passkeys: Passkey[] } = $props();

	// 確認は同時に1本だけ開く(迷いを増やさない)
	let confirmingId = $state<number | null>(null);
</script>

<ul class="passkey-list">
	{#each passkeys as p (p.id)}
		<li>
			<div class="row">
				<div class="info">
					<span class="meta">登録: {formatDate(p.created_at)}</span>
					<span class="meta">
						最終ログイン: {p.last_used_at ? formatDate(p.last_used_at) : PASSKEY_NEVER_USED}
					</span>
				</div>
				{#if confirmingId !== p.id}
					<button type="button" class="delete-trigger" onclick={() => (confirmingId = p.id)}>
						{DELETE_PASSKEY}
					</button>
				{/if}
			</div>
			{#if confirmingId === p.id}
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
						<span>
							注意: {DELETE_PASSKEY_WARNING}
							{#if passkeys.length === 1}
								{DELETE_LAST_PASSKEY_WARNING}
							{/if}
						</span>
					</p>
					<form class="confirm-actions" method="POST" action="/account?/delete">
						<input type="hidden" name="id" value={p.id} />
						<button type="submit" class="confirm-delete">{DELETE_PASSKEY}</button>
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
	.passkey-list {
		list-style: none;
		margin: 24px 0 0;
		padding: 0;
	}
	.passkey-list li {
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
		display: flex;
		flex-direction: column;
	}
	.meta {
		display: block;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
	}
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
