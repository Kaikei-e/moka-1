<script lang="ts">
	// パスキーログイン(ADR00021)。/auth はエッジ(Plecto フィルタ)の検証除外パス —
	// 認証前のブラウザに開かれた唯一の画面なので、記事の骨格は持たず(bare layout)、
	// 鍵の儀式だけを静かに給仕する。ボタンは常に 1 つ(未登録 = 作る / 登録済み = 開ける)。
	// 儀式は必ず BFF(/auth/* の +server.ts)経由 — ブラウザは moka-core と直接話さない。
	import { resolve } from '$app/paths';
	import DripIndicator from '$lib/components/DripIndicator.svelte';
	import { credentialToJSON, parseCreationOptions, parseRequestOptions } from '$lib/webauthn';
	import {
		AUTH_CREATE_PASSKEY,
		AUTH_CREATING_KEY,
		AUTH_LOGIN_FAILED,
		AUTH_OPENED,
		AUTH_REGISTER_FAILED,
		AUTH_STATUS_UNAVAILABLE,
		AUTH_UNLOCK,
		AUTH_VERIFYING_KEY,
		AUTH_WELCOME_BACK,
		AUTH_WELCOME_BACK_HINT,
		AUTH_WELCOME_NEW,
		AUTH_WELCOME_NEW_HINT
	} from '$lib/copy';

	let { data } = $props();

	let busy = $state(false);
	// 儀式完了 → 金の一滴(推論完了ではないが「鍵が開いた」一度きりの瞬間)→ ホームへ
	let opened = $state(false);
	let errorMessage = $state<string | null>(null);

	// BFF が写した静かな文言(error キー)を拾う。無ければ手元の文言に落とす
	async function quietError(res: Response, fallback: string): Promise<string> {
		try {
			const body = await res.json();
			if (typeof body.error === 'string') return body.error;
		} catch {
			// ボディが JSON でなくても文言は手元にある
		}
		return fallback;
	}

	async function createPasskey() {
		busy = true;
		errorMessage = null;
		try {
			const beginRes = await fetch('/auth/register/begin', { method: 'POST' });
			if (!beginRes.ok) {
				errorMessage = await quietError(beginRes, AUTH_REGISTER_FAILED);
				return;
			}
			const credential = await navigator.credentials.create(
				parseCreationOptions(await beginRes.json())
			);
			if (!(credential instanceof PublicKeyCredential)) {
				errorMessage = AUTH_REGISTER_FAILED;
				return;
			}
			const finishRes = await fetch('/auth/register/finish', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(credentialToJSON(credential))
			});
			if (!finishRes.ok) {
				errorMessage = await quietError(finishRes, AUTH_REGISTER_FAILED);
				return;
			}
			opened = true;
			window.location.assign(resolve('/'));
		} catch {
			// 儀式の中断(キャンセル等)も含めて静かに — 技術用語は出さない
			errorMessage = AUTH_REGISTER_FAILED;
		} finally {
			busy = false;
		}
	}

	async function unlock() {
		busy = true;
		errorMessage = null;
		try {
			const beginRes = await fetch('/auth/login/begin', { method: 'POST' });
			if (!beginRes.ok) {
				errorMessage = await quietError(beginRes, AUTH_LOGIN_FAILED);
				return;
			}
			const credential = await navigator.credentials.get(
				parseRequestOptions(await beginRes.json())
			);
			if (!(credential instanceof PublicKeyCredential)) {
				errorMessage = AUTH_LOGIN_FAILED;
				return;
			}
			const finishRes = await fetch('/auth/login/finish', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(credentialToJSON(credential))
			});
			if (!finishRes.ok) {
				errorMessage = await quietError(finishRes, AUTH_LOGIN_FAILED);
				return;
			}
			opened = true;
			window.location.assign(resolve('/'));
		} catch {
			errorMessage = AUTH_LOGIN_FAILED;
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head>
	<title>moka-1</title>
</svelte:head>

<div class="gate">
	<main class="panel">
		<p class="brand">moka-1</p>
		{#if data.registered === null}
			<p class="note" role="alert">{AUTH_STATUS_UNAVAILABLE}</p>
		{:else}
			<h1 class="welcome">{data.registered ? AUTH_WELCOME_BACK : AUTH_WELCOME_NEW}</h1>
			<p class="hint">{data.registered ? AUTH_WELCOME_BACK_HINT : AUTH_WELCOME_NEW_HINT}</p>
			{#if opened}
				<DripIndicator label={AUTH_OPENED} completed testid="auth-opened" />
			{:else if busy}
				<DripIndicator
					label={data.registered ? AUTH_VERIFYING_KEY : AUTH_CREATING_KEY}
					testid="auth-busy"
				/>
			{:else}
				<button class="primary" type="button" onclick={data.registered ? unlock : createPasskey}>
					{data.registered ? AUTH_UNLOCK : AUTH_CREATE_PASSKEY}
				</button>
			{/if}
			{#if errorMessage}
				<!-- エラーは紺紙金泥ブロック: kon 地 + kindei-bright + アイコン + 「失敗:」(§2.4) -->
				<p class="error" role="alert">
					<svg aria-hidden="true" width="14" height="14" viewBox="0 0 16 16" fill="none">
						<circle cx="8" cy="8" r="6.5" stroke="currentColor" stroke-width="1.4" />
						<path d="M8 4.8v4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
						<circle cx="8" cy="11.2" r="0.9" fill="currentColor" />
					</svg>
					失敗: {errorMessage}
				</p>
			{/if}
		{/if}
	</main>
</div>

<style>
	/* 静かな入口 — 記事の骨格を持たない一枚の kumoi 地に、小さな月白の面をひとつ置くだけ */
	.gate {
		min-height: 100dvh;
		display: grid;
		place-items: center;
		padding: 24px 20px;
	}
	.panel {
		width: 100%;
		max-width: 360px;
		background: var(--geppaku);
		border-radius: var(--radius-card);
		padding: 32px 24px;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 12px;
	}
	.brand {
		margin: 0 0 12px;
		font: 500 16px/1.5 var(--font-ui);
		color: var(--kon);
	}
	.welcome {
		margin: 0;
		font: 500 16px/1.8 var(--font-ui);
		color: var(--kon);
	}
	.hint {
		margin: 0 0 8px;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
	}
	.note {
		margin: 0;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
	}
	/* ruri は 1 画面 1 箇所(§1-5) — この画面ではこのボタンだけ */
	.primary {
		border: none;
		border-radius: var(--radius-control);
		background: var(--ruri);
		color: var(--geppaku);
		padding: 10px 16px;
		min-height: 44px;
		font: 500 14px/1 var(--font-ui);
		cursor: pointer;
		transition: background-color var(--dur-fast) var(--ease-calm);
	}
	.primary:hover {
		background: var(--ruri-deep);
	}
	.error {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 0;
		padding: 10px 14px;
		background: var(--kon);
		border-radius: var(--radius-card);
		font: 400 12px/1.6 var(--font-ui);
		color: var(--kindei-bright); /* kon 地の上のみ許可(§2.2) */
	}
	.error svg {
		flex: none;
	}
</style>
