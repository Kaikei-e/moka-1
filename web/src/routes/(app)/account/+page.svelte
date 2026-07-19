<script lang="ts">
	// アカウント: パスキー管理とログアウト(/account、ADR00021)。
	// ログアウトは WebAuthn ログインと違い JS が要らない操作だが、moka-core の Set-Cookie
	// 中継結果を見てから遷移したいので、パスキー削除(native form action)とは違い
	// fetch + ハードナビゲーションの JS 駆動にする。SvelteKit の goto()(SPA遷移)は使わない
	// — セッション失効直後の client-side fetch は Plecto のセッション認証フィルタに
	// 素通りしない場合があり(navigation 用 fetch が text/html を Accept しないと 302 でなく
	// 401 になる、ADR00021)、フルリロードの方が安全かつ意味的にも正しい(認証状態の
	// 全面切り替えなので、残った SPA 状態を持ち越さない)。
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import DripIndicator from '$lib/components/DripIndicator.svelte';
	import PasskeyList from '$lib/components/PasskeyList.svelte';
	import {
		ACCOUNT_TITLE,
		DELETED,
		LOGGING_OUT,
		LOGOUT,
		LOGOUT_FAILED,
		PASSKEYS_HEADING,
		PASSKEYS_UNAVAILABLE
	} from '$lib/copy';

	let { data, form } = $props();

	const deleted = $derived(page.url.searchParams.get('deleted') !== null);
	const deleteError = $derived(form?.scope === 'delete' ? form.message : null);

	let loggingOut = $state(false);
	let logoutError = $state<string | null>(null);

	async function logout() {
		loggingOut = true;
		logoutError = null;
		try {
			const res = await fetch('/account/logout', { method: 'POST' });
			if (!res.ok) {
				logoutError = LOGOUT_FAILED;
				return;
			}
			window.location.href = resolve('/auth/login');
		} catch {
			logoutError = LOGOUT_FAILED;
		} finally {
			loggingOut = false;
		}
	}
</script>

<svelte:head>
	<title>{ACCOUNT_TITLE} — moka-1</title>
</svelte:head>

<section class="account-page">
	<h1>{ACCOUNT_TITLE}</h1>

	<section class="block">
		<div class="block-head">
			<h2>{PASSKEYS_HEADING}</h2>
			<button type="button" class="logout-trigger" onclick={logout} disabled={loggingOut}>
				{#if loggingOut}
					<DripIndicator label={LOGGING_OUT} testid="logout-drip" />
				{:else}
					{LOGOUT}
				{/if}
			</button>
		</div>

		{#if logoutError}
			<p class="delete-error" role="alert">
				<svg aria-hidden="true" width="14" height="14" viewBox="0 0 16 16" fill="none">
					<circle cx="8" cy="8" r="6.5" stroke="currentColor" stroke-width="1.4" />
					<path d="M8 4.8v4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
					<circle cx="8" cy="11.2" r="0.9" fill="currentColor" />
				</svg>
				失敗: {logoutError}
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
			<p class="delete-error" role="alert">
				<svg aria-hidden="true" width="14" height="14" viewBox="0 0 16 16" fill="none">
					<circle cx="8" cy="8" r="6.5" stroke="currentColor" stroke-width="1.4" />
					<path d="M8 4.8v4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
					<circle cx="8" cy="11.2" r="0.9" fill="currentColor" />
				</svg>
				失敗: {deleteError}
			</p>
		{/if}

		{#if data.passkeysUnavailable}
			<p class="note">{PASSKEYS_UNAVAILABLE}</p>
		{:else}
			<PasskeyList passkeys={data.passkeys} />
		{/if}
	</section>
</section>

<style>
	.account-page {
		max-width: var(--measure-ja);
		margin: 0 auto;
	}
	h1 {
		margin: 0 0 24px;
		font: 500 18px/1.7 var(--font-ui);
	}
	.block-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 16px;
	}
	h2 {
		margin: 0;
		font: 500 14px/1.7 var(--font-ui);
		color: var(--kon);
	}
	.logout-trigger {
		min-height: 44px;
		padding: 0 14px;
		border: 1px solid var(--hatoba);
		border-radius: var(--radius-control);
		background: var(--geppaku);
		color: var(--kon);
		font: 500 12px/1 var(--font-ui);
		cursor: pointer;
	}
	.logout-trigger:disabled {
		cursor: default;
		opacity: 0.7;
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
		color: var(--kindei-bright);
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
