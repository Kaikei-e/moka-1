#!/usr/bin/env bash
# llm 完全停止時のフェイルソフト検証(M2): e2e-llm-mock を実際に止めて、
#   - 検索がテキスト側単独で 200 を返し続けること(tenets §2-6 — 増強は死んでも読める)
#   - Q&A が SSE の error イベント(HTTP 200 のまま)で静かに失敗すること
# を rag_failsoft.hurl で検証する。終了時(失敗時含む)に必ず e2e-llm-mock を再開する。
#
# 前提: e2e/README.md の手順で moka-core / e2e-fixtures / e2e-db / e2e-llm-mock が起動済み。
# 停止中は enrich.Scheduler の埋め込み/要約も失敗 attempt を積む(バックオフで回復する)ため、
# 他シナリオへの波及を避けて hurl 群・シェルシナリオ群の一番最後に実行すること。
# リポジトリルートから実行すること: bash e2e/hurl/core/rag_failsoft_e2e.sh
set -euo pipefail

HOST="${E2E_HOST:-http://localhost:8080}"
COMPOSE=(docker compose -f compose.yaml -f compose.e2e.yaml)

# 1. 質問対象の記事 id を先に取っておく(どの記事でもよい — 存在することだけが前提)
article_id=$(curl -sf "$HOST/api/v1/articles?limit=1" | jq -r '.articles[0].id')
if [ -z "$article_id" ] || [ "$article_id" = "null" ]; then
  echo "rag_failsoft_e2e: no articles found (run after feeds_and_articles.hurl)" >&2
  exit 1
fi
echo "rag_failsoft_e2e: article_id=$article_id"

# 2. llm(モック)を止める。以降 moka-core からの補完・埋め込みは接続エラーになる
"${COMPOSE[@]}" stop e2e-llm-mock
trap '"${COMPOSE[@]}" start e2e-llm-mock >/dev/null 2>&1 || true' EXIT
echo "rag_failsoft_e2e: e2e-llm-mock stopped"

# 3. 検索のテキスト単独 200 と Q&A の SSE error イベントを検証する
hurl --test --jobs 1 \
  --variable host="$HOST" \
  --variable article_id="$article_id" \
  e2e/hurl/core/rag_failsoft.hurl

echo "rag_failsoft_e2e: ok (e2e-llm-mock restarts on exit)"
