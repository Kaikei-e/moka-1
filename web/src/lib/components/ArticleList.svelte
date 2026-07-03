<script lang="ts">
	import { resolve } from '$app/paths';
	import type { Article } from '$lib/api/schemas';
	import { EMPTY_ARTICLES } from '$lib/copy';
	import { formatDate } from '$lib/format';

	let { articles, currentId = null }: { articles: Article[]; currentId?: number | null } = $props();
</script>

{#if articles.length === 0}
	<p class="empty">{EMPTY_ARTICLES}</p>
{:else}
	<nav aria-label="記事リスト">
		<ul class="articles">
			{#each articles as a (a.id)}
				<li>
					<a
						href={resolve('/articles/[id]', { id: String(a.id) })}
						aria-current={a.id === currentId ? 'page' : undefined}
					>
						<span class="title">{a.title}</span>
						{#if a.published_at}
							<span class="meta">{formatDate(a.published_at)}</span>
						{/if}
					</a>
				</li>
			{/each}
		</ul>
	</nav>
{/if}

<style>
	.empty {
		margin: 0;
		padding: 12px 16px;
		font: 400 14px/1.8 var(--font-ui);
		color: var(--konnezu);
	}
	.articles {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.articles a {
		display: block;
		padding: 10px 16px;
		text-decoration: none;
		color: var(--kon);
		border-bottom: 0.5px solid var(--hatoba);
		transition: background-color var(--dur-fast) var(--ease-calm);
	}
	.articles a:hover {
		background: var(--hatoba); /* 選択面(§2.1)。サイドバー地 geppaku と濃淡差を付ける */
	}
	.articles a[aria-current='page'] {
		background: var(--ruri-tint);
	}
	.title {
		display: block;
		font: 400 14px/1.7 var(--font-article);
	}
	.meta {
		display: block;
		margin-top: 2px;
		font: 400 12px/1.6 var(--font-ui);
		color: var(--konnezu);
	}
</style>
