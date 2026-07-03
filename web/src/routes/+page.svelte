<script lang="ts">
	// ホームの読書カラム: 記事未選択の空状態は招待(§7.2)
	import { PICK_ARTICLE } from '$lib/copy';
</script>

<svelte:head>
	<title>moka-1</title>
</svelte:head>

<div class="empty-reading">
	<p>{PICK_ARTICLE}</p>
</div>

<style>
	.empty-reading {
		display: grid;
		place-items: center;
		min-height: 40vh;
	}
	.empty-reading p {
		font: 400 14px/1.8 var(--font-ui);
		color: var(--konnezu);
	}
</style>
