<script lang="ts">
	// 登録は /feeds の named action(?/register)に集約(ホームの空状態からも同じ導線)。
	// あえて use:enhance を付けない: 他ルートへの POST は native に任せると
	// 成功(303 → /feeds)も失敗(/feeds 上でエラー表示)も1経路で済み、JS 無しでも同じに動く。
	// named にしたのは削除 action(?/delete)と同居させるため — default とは共存できない
	let { errorMessage = null }: { errorMessage?: string | null } = $props();
	const uid = $props.id();
</script>

<form class="register" method="POST" action="/feeds?/register">
	<label for="{uid}-url">フィードの URL</label>
	<div class="row">
		<input
			id="{uid}-url"
			name="url"
			type="url"
			required
			placeholder="https://example.com/feed.xml"
		/>
		<button type="submit">登録する</button>
	</div>
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
</form>

<style>
	.register label {
		display: block;
		margin-bottom: 6px;
		font: 500 12px/1.6 var(--font-ui);
		color: var(--konnezu);
	}
	.row {
		display: flex;
		gap: 8px;
	}
	.register input {
		flex: 1;
		min-width: 0;
		border: 1px solid var(--hatoba);
		border-radius: var(--radius-control);
		background: var(--geppaku);
		padding: 10px 12px;
		font: 400 14px/1.8 var(--font-ui);
		color: var(--kon);
	}
	.register button {
		flex: none;
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
	.register button:hover {
		background: var(--ruri-deep);
	}
	.error {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 8px 0 0;
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
