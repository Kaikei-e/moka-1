<script lang="ts">
	// Q&A(DESIGN_LANGUAGE §8.2)。回答はチャット吹き出しでなく fujinezu 面のフラットなブロック。
	// llm 連携までは質問を積み、正直に「準備ができていない」ドリップを返す
	import DripIndicator from './DripIndicator.svelte';
	import { ANSWER_PENDING } from '$lib/copy';

	let draft = $state('');
	let questions = $state<string[]>([]);

	function ask(e: SubmitEvent) {
		e.preventDefault();
		const q = draft.trim();
		if (!q) return;
		questions.push(q);
		draft = '';
	}
</script>

{#each questions as q, i (i)}
	<div class="qa">
		<p class="question">{q}</p>
		<div class="answer">
			<DripIndicator label={ANSWER_PENDING} />
		</div>
	</div>
{/each}

<form class="ask" onsubmit={ask}>
	<input
		type="text"
		placeholder="この記事について訊く…"
		aria-label="この記事について訊く"
		bind:value={draft}
	/>
	<button type="submit" aria-label="訊く">
		<svg aria-hidden="true" width="16" height="16" viewBox="0 0 16 16" fill="none">
			<path
				d="M2.5 8h10M9 4.5 13 8l-4 3.5"
				stroke="currentColor"
				stroke-width="1.5"
				stroke-linecap="round"
				stroke-linejoin="round"
			/>
		</svg>
	</button>
</form>

<style>
	.qa {
		border-top: 1px solid var(--hatoba);
		padding-top: 12px;
		margin-top: 16px;
	}
	.question {
		margin: 0 0 8px;
		font: 400 14px/1.8 var(--font-ui);
		color: var(--kon);
	}
	.answer {
		/* フラットなブロック — 面の濃淡差のみで区切る */
		background: var(--fujinezu);
		border-radius: var(--radius-card);
		padding: 12px;
	}
	.ask {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-top: 16px;
		background: var(--geppaku);
		border: 0.5px solid var(--hatoba);
		border-radius: 999px; /* pill(§4.2) */
		padding: 6px 6px 6px 16px;
	}
	.ask input {
		flex: 1;
		min-width: 0;
		border: none;
		background: transparent;
		font: 400 14px/1.8 var(--font-ui);
		color: var(--kon);
	}
	.ask input:focus-visible {
		outline: none;
		box-shadow: none; /* リングは pill 全体に付ける */
	}
	.ask:focus-within {
		box-shadow:
			0 0 0 2px var(--kumoi),
			0 0 0 4px var(--ruri);
	}
	.ask button {
		flex: none;
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		border: none;
		border-radius: 50%;
		background: transparent;
		color: var(--ruri);
		cursor: pointer;
	}
	.ask button:hover {
		color: var(--ruri-deep);
	}
</style>
