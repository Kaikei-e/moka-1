<script lang="ts">
	// 2ペイン骨格(DESIGN_LANGUAGE §4.3): サイドバー 280px + 読書カラム。
	// ブレークポイントは 900px の1つだけ。モバイルはサイドバーをドロワーに収納
	import '@fontsource/shippori-mincho/400.css';
	import '@fontsource/shippori-mincho/500.css';
	import '@fontsource/zen-kaku-gothic-new/400.css';
	import '@fontsource/zen-kaku-gothic-new/500.css';
	import '@fontsource/ibm-plex-mono/400.css';
	import '@fontsource/ibm-plex-mono/500.css';
	import '../app.css';
	import favicon from '$lib/assets/favicon.svg';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import ArticleList from '$lib/components/ArticleList.svelte';
	import FeedRegisterForm from '$lib/components/FeedRegisterForm.svelte';
	import { LIST_UNAVAILABLE } from '$lib/copy';

	let { data, children } = $props();

	let drawerOpen = $state(false);
	const currentId = $derived(page.params.id ? Number(page.params.id) : null);
	// ナビゲーションしたらドロワーを閉じる
	$effect(() => {
		void page.url.pathname;
		drawerOpen = false;
	});
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

<div class="shell">
	<header class="topbar">
		<button
			class="menu"
			aria-label="メニュー"
			aria-expanded={drawerOpen}
			onclick={() => (drawerOpen = !drawerOpen)}
		>
			<svg aria-hidden="true" width="18" height="18" viewBox="0 0 18 18" fill="none">
				<path d="M2 4.5h14M2 9h14M2 13.5h14" stroke="currentColor" stroke-width="1.5" />
			</svg>
		</button>
		<a class="brand" href={resolve('/')}>moka-1</a>
	</header>

	<aside class="sidebar" class:open={drawerOpen}>
		<div class="sidebar-head">
			<a class="brand" href={resolve('/')}>moka-1</a>
			<a class="feeds-link" href={resolve('/feeds')}>フィード管理</a>
		</div>
		{#if data.listUnavailable}
			<p class="side-note">{LIST_UNAVAILABLE}</p>
		{:else}
			<ArticleList articles={data.articles} nextCursor={data.nextCursor} {currentId} />
			{#if data.articles.length === 0}
				<div class="side-register">
					<FeedRegisterForm />
				</div>
			{/if}
		{/if}
	</aside>

	<main class="reading">
		{@render children()}
	</main>
</div>

<style>
	.shell {
		min-height: 100dvh;
	}
	.brand {
		font: 500 16px/1.5 var(--font-ui);
		color: var(--kon);
		text-decoration: none;
	}
	.topbar {
		display: flex;
		align-items: center;
		gap: 8px;
		height: 52px;
		padding: 0 12px;
		background: var(--geppaku);
		border-bottom: 0.5px solid var(--hatoba);
		position: sticky;
		top: 0;
		z-index: 2;
	}
	.menu {
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		border: none;
		background: transparent;
		color: var(--kon);
		cursor: pointer;
	}
	.sidebar {
		background: var(--geppaku);
		border-right: 0.5px solid var(--hatoba);
		overflow-y: auto;
	}
	.sidebar-head {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		padding: 16px;
		border-bottom: 0.5px solid var(--hatoba);
	}
	.feeds-link {
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
		text-decoration: none;
	}
	.feeds-link:hover {
		color: var(--kon);
	}
	.side-note {
		margin: 0;
		padding: 12px 16px;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
	}
	.side-register {
		padding: 12px 16px 16px;
	}
	.reading {
		padding: 24px 20px 32px;
	}

	/* モバイル(< 900px): ドロワー */
	@media (max-width: 899.98px) {
		.sidebar {
			position: fixed;
			top: 52px;
			bottom: 0;
			left: 0;
			width: min(var(--sidebar-w), 85vw);
			transform: translateX(-100%);
			transition: transform var(--dur-calm) var(--ease-calm);
			z-index: 3;
		}
		.sidebar.open {
			transform: translateX(0);
		}
		.sidebar-head .brand {
			display: none;
		}
	}

	/* デスクトップ(>= 900px): 常設サイドバー */
	@media (min-width: 900px) {
		.shell {
			display: grid;
			grid-template-columns: var(--sidebar-w) 1fr;
		}
		.topbar {
			display: none;
		}
		.sidebar {
			height: 100dvh;
			position: sticky;
			top: 0;
		}
		.reading {
			padding: 48px 48px 64px;
		}
	}
</style>
