---
name: grill-with-docs
description: Grilling session that challenges your plan against moka-1's documented design decisions (tenets, ADRs) and domain model, sharpens terminology, and updates documentation (CONTEXT.md, ADRs) inline as decisions crystallise. Use when the user wants to stress-test a plan against the project's language and documented decisions, or says 「設計を詰めて」「用語を固めて」「ドキュメントと突き合わせて」.
---

<what-to-do>

Interview me relentlessly about every aspect of this plan until we reach a shared understanding. Walk down each branch of the design tree, resolving dependencies between decisions one-by-one. For each question, provide your recommended answer.

Ask the questions one at a time, waiting for feedback on each question before continuing.

If a question can be answered by exploring the codebase, explore the codebase instead.

</what-to-do>

<supporting-info>

## Domain awareness

During codebase exploration, also look for existing documentation. For moka-1 the canonical
sources are `docs/tenets/moka-tenets.md`(設計原理・アーキテクチャ・決定事項のまとめ)and
`docs/adr/`(ADR00001.md からの連番), plus `CLAUDE.md`(規約とコマンドの要約)and
`CONTEXT.md` once it exists.

### File structure

moka-1 is a single context today:

```
/
├── CLAUDE.md                        ← 規約・コマンド・アーキテクチャ要約
├── CONTEXT.md                       ← 用語集(遅延作成)
├── docs/
│   ├── tenets/
│   │   └── moka-tenets.md           ← 設計原理 + 決定事項の正準まとめ
│   └── adr/
│       ├── template.md
│       ├── ADR00001.md              ← e.g. "スキーマ管理はAtlas、one-shot migrateジョブ"
│       └── ADR00007.md              ← e.g. "高速パスモデルをQwen3.5-4Bに確定"
├── core/                            # Go: API + エージェントループ
├── plecto/                          # エッジ: manifest + WASMフィルタ
└── web/                             # SvelteKit
```

If a `CONTEXT-MAP.md` exists at the root, the repo has grown multiple contexts (e.g. アプリ / エッジ / 評価 — see CONTEXT-FORMAT.md). Treat that as a signal to double-check against the minimalism tenets before accepting the split.

Create files lazily — only when you have something to write. If no `CONTEXT.md` exists, create one when the first term is resolved.

## During the session

### Challenge against the glossary

When the user uses a term that conflicts with the existing language in `CONTEXT.md`, call it out immediately. "Your glossary defines 高速パス as the summarise/tag LLM role, but you seem to mean Plecto's native fast path — which is it?"

### Sharpen fuzzy language

When the user uses vague or overloaded terms, propose a precise canonical term. "You're saying 'ワーカー' — do you mean the 常駐エージェント loop inside moka-core, or a one-shot job like migrate? Those have different lifecycles and only the former counts toward the 5-service cap." Watch especially for words that belong to Plecto's domain (filter, fast path, decision) leaking into moka-1's app context without qualification.

### Discuss concrete scenarios

When domain relationships are being discussed, stress-test them with specific scenarios. Invent scenarios that probe edge cases and force the user to be precise about the boundaries between concepts (e.g. "llm コンテナが落ちている間に新着記事が来た — コアパスはどこまで進み、`enrichment_status` は何になり、復帰後に誰が再処理する?").

### Cross-reference with code and the documented design

When the user states how something works, check whether the code and the documented design decisions (tenets §番号 / ADR番号) agree. If you find a contradiction, surface it: "You just said the recap needs a new service, but Tenet 3 caps resident services at 5 and ADR00001 says one-shot jobs are the only exception — which is right?"

### Update CONTEXT.md inline

When a term is resolved, update `CONTEXT.md` right there. Don't batch these up — capture them as they happen. Use the format in [CONTEXT-FORMAT.md](./CONTEXT-FORMAT.md).

`CONTEXT.md` should be totally devoid of implementation details. Do not treat `CONTEXT.md` as a spec, a scratch pad, or a repository for implementation decisions. It is a glossary and nothing else.

### Offer ADRs sparingly

Only offer to create an ADR when all three are true:

1. **Hard to reverse** — the cost of changing your mind later is meaningful
2. **Surprising without context** — a future reader will wonder "why did they do it this way?"
3. **The result of a real trade-off** — there were genuine alternatives and you picked one for specific reasons

If any of the three is missing, skip the ADR. For a full ADR (docs/adr/ 連番・日本語・template.md 準拠), hand off to the `moka-adr-writer` skill.

</supporting-info>
