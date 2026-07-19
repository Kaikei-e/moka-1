<script lang="ts">
	// ルート layout はフォント・グローバル CSS・favicon だけの薄い殻。
	// 2ペインの骨格(サイドバー + 読書カラム)は (app)/+layout.svelte が持ち、
	// /auth はこの殻の直下に置かれる = 記事の骨格を持たない(bare、ADR00021)。
	// bare の分岐を url から導くと layout load が全ナビゲーションで再実行され、
	// ArticleList の読み込み済みページが破棄されるため、route group の構造で分ける
	import '@fontsource/shippori-mincho/400.css';
	import '@fontsource/shippori-mincho/500.css';
	import '@fontsource/zen-kaku-gothic-new/400.css';
	import '@fontsource/zen-kaku-gothic-new/500.css';
	import '@fontsource/ibm-plex-mono/400.css';
	import '@fontsource/ibm-plex-mono/500.css';
	import '../app.css';
	import favicon from '$lib/assets/favicon.svg';

	let { children } = $props();
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

{@render children()}
