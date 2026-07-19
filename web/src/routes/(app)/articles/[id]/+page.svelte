<script lang="ts">
	// 読書ビュー: 本文(記事の声 = 明朝)が主役。AI 要素(要約・問い返し)は給仕として従属する。
	// 流れは タイトル → メタ → 要約カード → 本文 → 全文を取り寄せる → 訊く、の一本。
	// 対訳は LLM 翻訳の実装まで取り下げ中 — コンポーネントと規定は保持している。
	//
	// 全文取り寄せ: 明示ボタンのみが引き金(自動取得しない)。取得済みなら本文を置き換える。
	// 冪等(サーバー側で保存済みなら再取得しない)なので、成功後はボタンごと消して再クリックを防ぐ。
	//
	// 既読打刻: 読書ビューを開いた事実だけを fire-and-forget で moka-core に残す(冪等)。
	// SSR の load に副作用を持たせず、失敗は黙って握りつぶす — 読書を妨げない。
	//
	// SvelteKit は同一ルート(/articles/[id])内の遷移でこのコンポーネントインスタンスを
	// 再利用し、data だけ差し替える。取り寄せ状態は記事単位なので data.article.id の変化を
	// 検知してリセットする(既読打刻も同じ契機で記事ごとに一度ずつ飛ぶ)。
	import AskBar from '$lib/components/AskBar.svelte';
	import DripIndicator from '$lib/components/DripIndicator.svelte';
	import SummaryCard from '$lib/components/SummaryCard.svelte';
	import TagList from '$lib/components/TagList.svelte';
	import { toParagraphs } from '$lib/article-text';
	import { sanitizeArticleHtml } from '$lib/sanitize';
	import { hostnameOf, isSafeExternalUrl } from '$lib/url';
	import { formatDate } from '$lib/format';
	import { FETCH_FULLTEXT, FETCHING_FULLTEXT } from '$lib/copy';

	let { data } = $props();

	let fullText = $state<string | null>(null);
	let fetching = $state(false);
	let fetchError = $state<string | null>(null);

	$effect(() => {
		const id = data.article.id; // 依存の確立(記事切り替えのたびにリセットする)
		fullText = null;
		fetching = false;
		fetchError = null;
		// 既読打刻: fire-and-forget、フェイルソフト(エラーは読書に一切波及させない)
		void fetch(`/articles/${id}/read`, { method: 'POST' }).catch(() => {});
	});

	const paragraphs = $derived(toParagraphs(fullText ?? data.article.content));
	// 取り寄せ済みなら構造(見出し・リスト・コードブロック等)を保ったまま描画する。
	// RSS 由来の content は構造情報を持たないため対象外(タグを剥がしたプレーン段落のまま)
	const fullTextHtml = $derived(fullText ? sanitizeArticleHtml(fullText) : null);
	// メタ行の店の名(リスト行と同じ手がかり): フィード名、無ければホスト名
	const sourceLabel = $derived(data.article.feed_title ?? hostnameOf(data.article.url) ?? '');

	async function fetchFullText() {
		// 応答待ちの間に記事が切り替わったら(コンポーネントは再利用される)応答を丸ごと捨てる
		const id = data.article.id;
		fetching = true;
		fetchError = null;
		try {
			const res = await fetch(`/articles/${id}/fulltext`, { method: 'POST' });
			const body = await res.json();
			if (id !== data.article.id) return;
			if (!res.ok) {
				fetchError = body.error ?? '取り寄せに失敗しました。再試行してください';
				return;
			}
			fullText = body.fulltext.text;
		} catch {
			if (id !== data.article.id) return;
			fetchError = '取り寄せに失敗しました。再試行してください';
		} finally {
			if (id === data.article.id) fetching = false;
		}
	}
</script>

<svelte:head>
	<title>{data.article.title} — moka-1</title>
</svelte:head>

<article class="reading-col">
	<header class="article-header">
		<h1>{data.article.title}</h1>
		<p class="meta">
			{#if sourceLabel}
				<span class="source">{sourceLabel}</span>
			{/if}
			{#if data.article.published_at}
				<time datetime={data.article.published_at}>{formatDate(data.article.published_at)}</time>
			{/if}
			{#if isSafeExternalUrl(data.article.url)}
				<a href={data.article.url} target="_blank" rel="noopener noreferrer">原文を開く</a>
			{/if}
		</p>
	</header>

	<SummaryCard articleId={data.article.id} />
	<TagList articleId={data.article.id} />

	{#if fullTextHtml}
		<!-- eslint-disable-next-line svelte/no-at-html-tags -- fullTextHtml は sanitizeArticleHtml (DOMPurify、許可タグ限定) を通した後の値のみ -->
		<div class="body article-html">{@html fullTextHtml}</div>
	{:else}
		<div class="body">
			{#each paragraphs as p, i (i)}
				<p>{p}</p>
			{/each}
		</div>
	{/if}

	{#if fullText === null}
		<div class="fulltext-zone">
			{#if fetching}
				<DripIndicator label={FETCHING_FULLTEXT} testid="fulltext-drip" />
			{:else}
				<button class="fulltext-button" onclick={fetchFullText}>{FETCH_FULLTEXT}</button>
			{/if}
			{#if fetchError}
				<p class="fulltext-error" role="alert">
					<span aria-hidden="true">⚠</span>
					失敗: {fetchError}
				</p>
			{/if}
		</div>
	{/if}

	<!-- 訊く(問い返し): 読み終わりに置く。回答は AI 生成物として fujinezu 面に載る(§8.2) -->
	<footer class="ask-zone">
		<AskBar articleId={data.article.id} />
	</footer>
</article>

<style>
	.reading-col {
		max-width: var(--measure-ja);
		margin: 0 auto;
	}
	.article-header h1 {
		margin: 0 0 8px;
		font: 500 22px/1.6 var(--font-article);
	}
	.meta {
		margin: 0 0 24px;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
	}
	.body {
		margin-top: 24px;
	}
	.body p {
		margin-block: 1em;
		font: 400 15px/2.05 var(--font-article);
		color: var(--kon);
	}

	/* 本文の後、静かな取り寄せ導線(本文が主役 — メタ行に混ぜず、読み終わりに置く) */
	.fulltext-zone {
		margin-top: 32px;
	}
	/* 訊くは読書の流れの最後。上との間はマージンのみ — 罫線は AskBar 側の質問区切りに任せる */
	.ask-zone {
		margin-top: 32px;
	}
	.fulltext-button {
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
	/* エラー = 紺紙金泥ブロック(DESIGN_LANGUAGE §2.4)。読書カラム内にインラインで置く */
	.fulltext-error {
		margin: 12px 0 0;
		padding: 12px 14px;
		border-radius: var(--radius-card);
		background: var(--kon);
		color: var(--kindei-bright);
		font: 400 12px/1.6 var(--font-ui);
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
</style>
