<script lang="ts">
	// 署名アニメーション「ドリップ」(DESIGN_LANGUAGE §6.2)。待ち全般(推論・取り寄せ)を示す
	// moka-1 唯一の装飾。回転スピナーの代替 — 装飾は滴、真実はテキスト。
	//
	// 構成: 間のリズム(不等間隔で3滴/周期)→ 着水時にグーイー合流(SVG フィルタ)→
	// 墨流しの着水(非対称なインクの拡がり)。金の一滴(completed)は推論完了専用の
	// 一度きりの静止表現で、待ちのループには使わない(金は動かない)。
	let {
		label,
		testid,
		completed = false
	}: { label: string; testid?: string; completed?: boolean } = $props();

	const uid = $props.id();
</script>

<div
	class="drip-indicator"
	role="status"
	data-testid={testid}
	data-completed={completed || undefined}
>
	<svg class="glyph" class:completed aria-hidden="true" viewBox="0 0 20 34" width="20" height="34">
		<defs>
			<filter id="goo-{uid}" x="-60%" y="-60%" width="220%" height="220%">
				<feGaussianBlur in="SourceGraphic" stdDeviation="1.4" result="blur" />
				<feColorMatrix
					in="blur"
					mode="matrix"
					values="1 0 0 0 0  0 1 0 0 0  0 0 1 0 0  0 0 0 20 -8"
					result="goo"
				/>
				<feComposite in="SourceGraphic" in2="goo" operator="atop" />
			</filter>
		</defs>
		<g filter={completed ? undefined : `url(#goo-${uid})`}>
			<circle class="drop" cx="10" cy="4" r="3" />
			<rect class="pool" x="0" y="30" width="20" height="2" rx="1" />
		</g>
		<ellipse class="bloom" cx="10" cy="31" rx="2.4" ry="1.2" />
	</svg>
	<span class="label">{label}</span>
</div>

<style>
	.drip-indicator {
		display: flex;
		align-items: center;
		gap: 12px;
		color: var(--konnezu);
		font: 400 12px/1.6 var(--font-ui);
	}
	.glyph {
		flex: none;
		overflow: visible;
	}
	.drop {
		fill: var(--ruri);
		opacity: 0;
		transform-box: fill-box;
		transform-origin: center;
		animation: drip-rhythm 5.4s linear infinite;
	}
	/* 液面は静止時は淡く(孤立した線に見えない)、着水の瞬間だけ明るくなる — 呼吸させる */
	.pool {
		fill: var(--ruri);
		opacity: 0.35;
		animation: pool-breathe 5.4s linear infinite;
	}
	.bloom {
		fill: var(--ruri);
		opacity: 0;
		transform-box: fill-box;
		transform-origin: center;
		animation: ink-bloom 5.4s linear infinite;
	}

	/* 金の一滴 — 推論完了の瞬間だけの静止表現(§7.1「金は動かない」)。ループには使わない */
	.glyph.completed .drop {
		opacity: 1;
		transform: translateY(26px);
		animation: none;
		fill: var(--kindei-bright);
	}
	.glyph.completed .pool {
		fill: var(--kindei-bright);
		opacity: 1;
		animation: none;
	}
	.glyph.completed .bloom {
		animation: none;
		opacity: 0;
	}

	/* 間のリズム — 3滴/5.4秒を不等間隔に配置(長い間 → 二連の短い間)。沈黙も設計素材にする */
	@keyframes drip-rhythm {
		0%,
		3% {
			transform: translateY(0);
			opacity: 0;
		}
		5% {
			opacity: 1;
		}
		18% {
			transform: translateY(26px);
			opacity: 1;
		}
		20% {
			transform: translateY(26px);
			opacity: 0;
		}
		20.01%,
		53% {
			transform: translateY(0);
			opacity: 0;
		}
		55% {
			opacity: 1;
		}
		66% {
			transform: translateY(26px);
			opacity: 1;
		}
		68% {
			transform: translateY(26px);
			opacity: 0;
		}
		68.01%,
		76% {
			transform: translateY(0);
			opacity: 0;
		}
		78% {
			opacity: 1;
		}
		90% {
			transform: translateY(26px);
			opacity: 1;
		}
		92% {
			transform: translateY(26px);
			opacity: 0;
		}
		92.01%,
		100% {
			transform: translateY(0);
			opacity: 0;
		}
	}

	/* 液面の呼吸 — drip-rhythm の着水タイミングと同期して明るくなる */
	@keyframes pool-breathe {
		0%,
		16% {
			opacity: 0.35;
		}
		19% {
			opacity: 1;
		}
		24%,
		64% {
			opacity: 0.35;
		}
		67% {
			opacity: 1;
		}
		72%,
		88% {
			opacity: 0.35;
		}
		91% {
			opacity: 1;
		}
		96%,
		100% {
			opacity: 0.35;
		}
	}

	/* 墨流しの着水 — 着水ごとに非対称な向きへ拡がって消える(同心円のリップルにしない) */
	@keyframes ink-bloom {
		0%,
		19% {
			opacity: 0;
			transform: translateX(0) scale(1);
		}
		21% {
			opacity: 0.9;
			transform: translateX(0) scale(1);
		}
		28% {
			opacity: 0;
			transform: translateX(3px) scale(2.4);
		}
		28.01%,
		67% {
			opacity: 0;
			transform: translateX(0) scale(1);
		}
		69% {
			opacity: 0.9;
			transform: translateX(0) scale(1);
		}
		76% {
			opacity: 0;
			transform: translateX(-2.5px) scale(2.2);
		}
		76.01%,
		91% {
			opacity: 0;
			transform: translateX(0) scale(1);
		}
		93% {
			opacity: 0.9;
			transform: translateX(0) scale(1);
		}
		99% {
			opacity: 0;
			transform: translateX(2px) scale(2.6);
		}
		100% {
			opacity: 0;
			transform: translateX(0) scale(1);
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.drop,
		.bloom {
			animation: none;
		}
		.pool {
			animation: none;
			opacity: 0.35;
		}
	}
</style>
