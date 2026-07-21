#!/usr/bin/env bash
# 問い返し Q&A の文脈検索クエリが対象記事のタイトルを連結して構成されることを検証する
# (RAG 精度改善 変更1)。独自フィード(feed-qa-context.xml、同一タイトル・別トピックの
# 2記事)を登録するので、他の厳密なカウントアサーションを崩さないよう enrich_e2e.sh /
# scheduler_e2e.sh の後、rag_failsoft_e2e.sh(e2e-llm-mock を止める)より前に実行すること。
#
# 前提: e2e/README.md の手順で moka-core / e2e-fixtures / e2e-db / e2e-llm-mock が
# 起動済み、フレッシュ DB。ベクトル側(article_embeddings)が効くまで enrich.Scheduler の
# 埋め込みを待つ必要がある(feed-qa-context.xml 側にも title 由来の trigram 手がかりがあるが、
# 埋め込みが済んでいるほうがクエリ構成変更の効果がより安定して観測できる)。
# リポジトリルートから実行すること: bash e2e/hurl/core/qa_context_relevance_e2e.sh
set -euo pipefail

HOST="${E2E_HOST:-http://localhost:8080}"
FIXTURE_URL="${QA_CONTEXT_FIXTURE_URL:-http://e2e-fixtures/feed-qa-context.xml}"

# 1. フィードを登録(同期・API起点)
feed_id=$(curl -sf -X POST "$HOST/api/v1/feeds" \
  -H 'Content-Type: application/json' \
  -d "{\"url\": \"$FIXTURE_URL\"}" | jq -r '.feed.id')
echo "qa_context_relevance_e2e: registered feed_id=$feed_id"

# 2. guid で対象記事(記事1)の id を引く(一覧の先頭には頼れない — enrich_e2e.sh と同じ理由)
target_article_id=$(curl -sf "$HOST/api/v1/articles?limit=200" \
  | jq -r '.articles[] | select(.guid=="urn:moka-e2e-qactx:1") | .id')
if [ -z "$target_article_id" ]; then
  echo "qa_context_relevance_e2e: could not find the target article by guid" >&2
  exit 1
fi
echo "qa_context_relevance_e2e: target_article_id=$target_article_id"

# 3. ベクトル側(article_embeddings)が対象記事を埋め込み終わるまで待つ。テキスト側
#    (pg_trgm)だけでも観測できるはずのシグナルだが、埋め込みが効いた通常状態で検証する
i=0
until curl -sf "$HOST/api/v1/search?q=Zyloforge%20Marmalade" | jq -e '.items | length >= 1' >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -ge 30 ]; then
    echo "qa_context_relevance_e2e: timed out waiting for embeddings" >&2
    exit 1
  fi
  sleep 2
done

# 4. 質問(トピック語なし)へのクエリ配線を Hurl で検証する
hurl --test --jobs 1 \
  --variable host="$HOST" \
  --variable target_article_id="$target_article_id" \
  --variable distractor_marker="qactx-2" \
  e2e/hurl/core/qa_context_relevance.hurl
