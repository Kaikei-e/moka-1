<script lang="ts">
	// 読書ビュー: 本文(記事の声 = 明朝)が主役。AI 要素(要約・対訳・Q&A)は給仕として従属する。
	// 対訳は左右2カラム(モバイルは段落交互)。未訳段落にはドリップを段落位置に置く(§5.3)
	//
	// 全文取り寄せ: 明示ボタンのみが引き金(自動取得しない)。取得済みなら本文を置き換える —
	// 対訳の「原文」カラムにも同じ paragraphs が使われるので、取り寄せ後は対訳にも反映される。
	// 冪等(サーバー側で保存済みなら再取得しない)なので、成功後はボタンごと消して再クリックを防ぐ。
	//
	// SvelteKit は同一ルート(/articles/[id])内の遷移でこのコンポーネントインスタンスを
	// 再利用し、data だけ差し替える。そのため取り寄せ状態はローカル $state に持たせたままだと
	// 記事を切り替えても前の記事の全文が残ってしまう — data.article.id の変化を検知して
	// リセットする(§5.3 の未訳表示・取り寄せ表示のどちらも記事単位の状態であるため)。
	import AskBar from '$lib/components/AskBar.svelte';
	import DripIndicator from '$lib/components/DripIndicator.svelte';
	import SummaryCard from '$lib/components/SummaryCard.svelte';
	import { toParagraphs } from '$lib/article-text';
	import { sanitizeArticleHtml } from '$lib/sanitize';
	import { formatDate } from '$lib/format';
	import { UNTRANSLATED, FETCH_FULLTEXT, FETCHING_FULLTEXT } from '$lib/copy';

	let { data } = $props();

	let taiyaku = $state(false);
	let fullText = $state<string | null>(null);
	let fetching = $state(false);
	let fetchError = $state<string | null>(null);

	$effect(() => {
		const id = data.article.id; // 依存の確立(記事切り替えのたびにリセットする)
		void id;
		taiyaku = false;
		fullText = null;
		fetching = false;
		fetchError = null;
	});

	// 対訳(段落ペア表示)は常にプレーン段落の粒度で扱う。取り寄せ済みなら全文が原文カラムになる
	const paragraphs = $derived(toParagraphs(fullText ?? data.article.content));
	// 通常表示は取り寄せ済みなら構造(見出し・リスト・コードブロック等)を保ったまま描画する。
	// RSS 由来の content は構造情報を持たないため対象外(タグを剥がしたプレーン段落のまま)
	const fullTextHtml = $derived(fullText ? sanitizeArticleHtml(fullText) : null);

	async function fetchFullText() {
		fetching = true;
		fetchError = null;
		try {
			const res = await fetch(`/articles/${data.article.id}/fulltext`, { method: 'POST' });
			const body = await res.json();
			if (!res.ok) {
				fetchError = body.error ?? '取り寄せに失敗しました。再試行してください';
				return;
			}
			fullText = body.fulltext.text;
		} catch {
			fetchError = '取り寄せに失敗しました。再試行してください';
		} finally {
			fetching = false;
		}
	}
</script>

<svelte:head>
	<title>{data.article.title} — moka-1</title>
</svelte:head>

<article class="reading-col" class:wide={taiyaku}>
	<header class="article-header">
		<h1>{data.article.title}</h1>
		<div class="meta-row">
			<p class="meta">
				{#if data.article.published_at}
					<time datetime={data.article.published_at}>{formatDate(data.article.published_at)}</time>
				{/if}
				<a href={data.article.url} target="_blank" rel="noopener noreferrer">原文を開く</a>
			</p>
			<div class="article-actions">
				{#if fullText === null && !fetching}
					<button class="fulltext-button" onclick={fetchFullText}>{FETCH_FULLTEXT}</button>
				{/if}
				<button class="taiyaku-toggle" aria-pressed={taiyaku} onclick={() => (taiyaku = !taiyaku)}>
					対訳
				</button>
			</div>
		</div>
		{#if fetching}
			<DripIndicator label={FETCHING_FULLTEXT} testid="fulltext-drip" />
		{/if}
		{#if fetchError}
			<p class="fulltext-error" role="alert">
				<span aria-hidden="true">⚠</span>
				失敗: {fetchError}
			</p>
		{/if}
	</header>

	<SummaryCard />

	{#if taiyaku}
		<div class="pairs">
			{#each paragraphs as p, i (i)}
				<div class="pair">
					<p class="original">{p}</p>
					<div class="translation">
						<span class="yaku-label">訳</span>
						<DripIndicator label={UNTRANSLATED} testid="untranslated-drip" />
					</div>
				</div>
			{/each}
		</div>
	{:else if fullTextHtml}
		<!-- eslint-disable-next-line svelte/no-at-html-tags -- fullTextHtml は sanitizeArticleHtml (DOMPurify、許可タグ限定) を通した後の値のみ -->
		<div class="body article-html">{@html fullTextHtml}</div>
	{:else}
		<div class="body">
			{#each paragraphs as p, i (i)}
				<p>{p}</p>
			{/each}
		</div>
	{/if}

	<footer class="ask-dock">
		<AskBar />
	</footer>
</article>

<style>
	.reading-col {
		max-width: var(--measure-ja);
		margin: 0 auto;
	}
	/* 対訳は2カラム分の幅を使う(測度は片側で維持される) */
	.reading-col.wide {
		max-width: calc(var(--measure-ja) * 2 + 32px);
	}
	.article-header h1 {
		margin: 0 0 8px;
		font: 500 22px/1.6 var(--font-article);
	}
	.meta-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 16px;
		margin-bottom: 24px;
	}
	.meta {
		margin: 0;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
		display: flex;
		gap: 12px;
	}
	.article-actions {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.taiyaku-toggle,
	.fulltext-button {
		flex: none;
		min-height: 44px;
		padding: 0 14px;
		border: 1px solid var(--hatoba);
		border-radius: var(--radius-control);
		background: var(--geppaku);
		color: var(--kon);
		font: 500 12px/1 var(--font-ui);
		cursor: pointer;
		transition: background-color var(--dur-fast) var(--ease-calm);
	}
	.taiyaku-toggle[aria-pressed='true'] {
		background: var(--ruri-tint);
	}
	/* エラー = 紺紙金泥ブロック(DESIGN_LANGUAGE §2.4)。読書カラム内にインラインで置く */
	.fulltext-error {
		margin: 12px 0 0;
		padding: 12px 14px;
		border-radius: var(--radius-card);
		background: var(--kon);
		color: var(--kindei-bright);
		font: 400 12px/1.6 var(--font-ui);
	}
	.body {
		margin-top: 24px;
	}
	.body p,
	.original {
		margin-block: 1em;
		font: 400 15px/2.05 var(--font-article);
		color: var(--kon);
	}

	/* 取り寄せた全文の構造化描画({@html} で流し込むので :global で子要素にスタイルを当てる) */
	.article-html :global(h2) {
		margin: 1.6em 0 0.6em;
		font: 500 18px/1.7 var(--font-article);
		color: var(--kon);
	}
	.article-html :global(h3),
	.article-html :global(h4),
	.article-html :global(h5),
	.article-html :global(h6) {
		margin: 1.4em 0 0.5em;
		font: 500 16px/1.7 var(--font-article);
		color: var(--kon);
	}
	.article-html :global(p) {
		margin-block: 1em;
		font: 400 15px/2.05 var(--font-article);
		color: var(--kon);
	}
	.article-html :global(ul),
	.article-html :global(ol) {
		margin: 1em 0;
		padding-inline-start: 1.4em;
		font: 400 15px/2.05 var(--font-article);
		color: var(--kon);
	}
	.article-html :global(li) {
		margin-block: 0.3em;
	}
	.article-html :global(strong) {
		font-weight: 500; /* ウェイトは 400/500 のみ(§3.1) */
	}
	.article-html :global(a) {
		color: var(--ruri);
		text-decoration: underline;
		text-underline-offset: 2px;
	}
	/* 引用は片側ボーダーのみ、角丸は併用しない(§4.2) */
	.article-html :global(blockquote) {
		margin: 1em 0;
		padding: 2px 0 2px 16px;
		border-left: 2px solid var(--hatoba);
		color: var(--konnezu);
		font-style: italic;
	}
	.article-html :global(pre) {
		margin: 1em 0;
		padding: 12px;
		background: var(--geppaku);
		border: 1px solid var(--hatoba);
		border-radius: var(--radius-control);
		overflow-x: auto;
	}
	.article-html :global(code) {
		font: 400 13px/1.7 var(--font-code);
		color: var(--kon);
	}
	.article-html :global(pre code) {
		background: none;
		padding: 0;
	}
	.article-html :global(p code),
	.article-html :global(li code) {
		background: var(--geppaku);
		padding: 1px 4px;
		border-radius: 4px;
	}
	.pairs {
		margin-top: 24px;
	}
	.pair {
		margin-block: 1em;
	}
	/* 訳文は AI 生成物 = fujinezu 面(§5.1)。ボーダーなし — 濃淡差のみで区切る */
	.translation {
		background: var(--fujinezu);
		border-radius: var(--radius-card);
		padding: 12px;
	}
	.yaku-label {
		display: inline-block;
		margin-bottom: 4px;
		font: 500 11px/1.5 var(--font-ui);
		color: var(--kindei); /* 金泥の印(§5.2) */
	}
	.ask-dock {
		margin-top: 32px;
	}

	@media (min-width: 900px) {
		.pair {
			display: grid;
			grid-template-columns: 1fr 1fr;
			gap: 32px;
			align-items: start;
		}
		.pair:hover {
			background: var(--hatoba); /* 段落ペアの同期ハイライト(§5.1) */
		}
		/* 「訳」の金泥の印はモバイルの段落交互のみ(§5.2)。金がひしめかない(原則5) */
		.yaku-label {
			display: none;
		}
	}

	/* モバイル: 入力バーは画面下部に固定(safe-area 考慮、§4.3) */
	@media (max-width: 899.98px) {
		.ask-dock {
			position: sticky;
			bottom: 0;
			padding: 8px 0 calc(8px + env(safe-area-inset-bottom));
			background: var(--kumoi);
		}
	}
</style>
