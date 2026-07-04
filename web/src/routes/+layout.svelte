<script lang="ts">
	// 2ペイン骨格(DESIGN_LANGUAGE §4.3): サイドバー 280px + 読書カラム。
	// ブレークポイントは 900px の1つだけ。モバイルはドロワーを持たず、記事リスト⇄読書ビューを
	// マスター・ディテール式のプッシュ遷移(スライド)で切り替える一枚絵にする(v3.2.0)。
	// ArticleList は常にマウントしたまま CSS で表示/非表示を切り替えるので、記事へ遷移しても
	// スクロール位置・無限スクロールの読み込み状態がリセットされない。
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
	import { topbarMode } from '$lib/mobile-nav';

	let { data, children } = $props();

	let menuOpen = $state(false);
	const currentId = $derived(page.params.id ? Number(page.params.id) : null);
	const mode = $derived(topbarMode(page.url.pathname));
	const showReading = $derived(mode === 'back');
	// ナビゲーションしたら「…」ポップオーバーを閉じる
	$effect(() => {
		void page.url.pathname;
		menuOpen = false;
	});
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

<div class="shell">
	<header class="topbar">
		{#if showReading}
			<a class="back" href={resolve('/')}>← 戻る</a>
		{:else}
			<a class="brand" href={resolve('/')}>moka-1</a>
			<div class="topbar-menu">
				<button
					class="menu-trigger"
					type="button"
					aria-haspopup="true"
					aria-expanded={menuOpen}
					aria-label="メニュー"
					onclick={() => (menuOpen = !menuOpen)}
				>
					…
				</button>
				{#if menuOpen}
					<div class="popover" role="menu">
						<a role="menuitem" class="popover-item" href={resolve('/feeds')}>フィード管理</a>
					</div>
				{/if}
			</div>
		{/if}
	</header>

	<aside class="sidebar" class:sidebar-hidden={showReading}>
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

	<main class="reading" class:reading-visible={showReading}>
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
		justify-content: space-between;
		gap: 8px;
		height: 52px;
		padding: 0 12px;
		background: var(--geppaku);
		border-bottom: 0.5px solid var(--hatoba);
		position: sticky;
		top: 0;
		z-index: 2;
	}
	.back {
		display: flex;
		align-items: center;
		height: 44px;
		padding: 0 8px;
		font: 500 14px/1 var(--font-ui);
		color: var(--kon);
		text-decoration: none;
	}
	.topbar-menu {
		position: relative;
	}
	.menu-trigger {
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		border: none;
		background: transparent;
		color: var(--kon);
		font: 500 18px/1 var(--font-ui);
		cursor: pointer;
	}
	.popover {
		position: absolute;
		top: 48px;
		right: 0;
		min-width: 160px;
		background: var(--geppaku);
		border: 0.5px solid var(--hatoba);
		border-radius: var(--radius-control);
		overflow: hidden;
		z-index: 4;
	}
	.popover-item {
		display: flex;
		min-height: 44px;
		padding: 0 16px;
		align-items: center;
		font: 400 13px/1.8 var(--font-ui);
		color: var(--kon);
		text-decoration: none;
	}
	.popover-item:hover {
		background: var(--hatoba);
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

	/* モバイル(< 900px): 記事リスト⇄読書ビューのマスター・ディテール式プッシュ遷移 */
	@media (max-width: 899.98px) {
		.sidebar {
			position: fixed;
			top: 52px;
			bottom: 0;
			left: 0;
			width: 100%;
			transform: translateX(0);
			visibility: visible;
			transition:
				transform var(--dur-calm) var(--ease-calm),
				visibility 0s linear 0s;
			z-index: 1;
		}
		.sidebar.sidebar-hidden {
			transform: translateX(-100%);
			visibility: hidden;
			transition:
				transform var(--dur-calm) var(--ease-calm),
				visibility 0s linear var(--dur-calm);
		}
		.sidebar-head {
			display: none;
		}

		.reading {
			position: fixed;
			top: 52px;
			bottom: 0;
			left: 0;
			width: 100%;
			overflow-y: auto;
			transform: translateX(100%);
			visibility: hidden;
			transition:
				transform var(--dur-calm) var(--ease-calm),
				visibility 0s linear var(--dur-calm);
		}
		.reading.reading-visible {
			transform: translateX(0);
			visibility: visible;
			transition:
				transform var(--dur-calm) var(--ease-calm),
				visibility 0s linear 0s;
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
