<script lang="ts">
	// 要約カード(DESIGN_LANGUAGE §8.1)。AI の声なので fujinezu 面 + ゴシック、印は金泥。
	// llm 連携までは正直に「準備ができていない」ドリップを常駐させる(偽の要約を捏造しない)
	import DripIndicator from './DripIndicator.svelte';
	import { SUMMARY_PENDING } from '$lib/copy';
</script>

<section class="summary-card">
	<h2 class="card-label">
		<svg aria-hidden="true" width="14" height="14" viewBox="0 0 16 16" fill="none">
			<path
				d="M3 6h8v3.5A3.5 3.5 0 0 1 7.5 13h-1A3.5 3.5 0 0 1 3 9.5V6Z"
				stroke="currentColor"
				stroke-width="1.4"
			/>
			<path d="M11 7h1a1.6 1.6 0 0 1 0 3.2h-1" stroke="currentColor" stroke-width="1.4" />
		</svg>
		moka による要約
	</h2>
	<DripIndicator label={SUMMARY_PENDING} testid="summary-drip" />
</section>

<style>
	.summary-card {
		/* ボーダーなし — 面の濃淡差のみで区切る(§8.1) */
		background: var(--fujinezu);
		border-radius: var(--radius-card);
		padding: 14px;
	}
	.card-label {
		display: flex;
		align-items: center;
		gap: 6px;
		margin: 0 0 10px;
		font: 500 11px/1.5 var(--font-ui);
		color: var(--kindei); /* moka の印 = 金泥 */
	}
</style>
