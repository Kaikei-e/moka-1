<script lang="ts">
	// ホームの読書カラム: 記事未選択の空状態は招待(§7.2)。
	// 今日のハイライトの静的予告を一文だけ添える — 操作も進行表現も付けない(静かな無為)
	import { HIGHLIGHT_FORECAST, PICK_ARTICLE } from '$lib/copy';
</script>

<svelte:head>
	<title>moka-1</title>
</svelte:head>

<div class="empty-reading">
	<p class="invite">{PICK_ARTICLE}</p>
	<p class="forecast">{HIGHLIGHT_FORECAST}</p>
</div>

<style>
	.empty-reading {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 12px;
		min-height: 60vh;
		text-align: center;
		padding: 0 20px;
	}
	.invite {
		margin: 0;
		font: 400 14px/1.8 var(--font-ui);
		color: var(--konnezu);
	}
	.forecast {
		margin: 0;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
	}
</style>
