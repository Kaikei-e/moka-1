#!/usr/bin/env bash
# enrich.Scheduler(常駐エージェントループの濃縮ステップ、tenets §3.2 step3)が、人間・
# API起点の要約/タグ生成リクエスト無しに、新着記事へ自動で summary/tags を付与することを
# 検証する。このスクリプトは POST .../summary / POST .../tags を一度も呼ばない —
# それでも中身が見える(GET のみで確認できる)ことそのものが M1 の Done 条件の主張。
#
# 前提: e2e/README.md の手順で moka-core / e2e-fixtures / e2e-db / e2e-llm-mock が
# 起動済み、フレッシュ DB。他の厳密なカウントアサーション(feeds_and_articles.hurl 等)を
# 崩さないよう独自フィードを登録するので、scheduler_e2e.sh の直前(最後の方)に実行すること。
# リポジトリルートから実行すること: bash e2e/hurl/core/enrich_e2e.sh
set -euo pipefail

HOST="${E2E_HOST:-http://localhost:8080}"
FIXTURE_URL="${ENRICH_FIXTURE_URL:-http://e2e-fixtures/feed-enrich.xml}"

# 1. フィードを登録(同期・API起点。ここまではユーザー操作の代替 — 要約/タグへの
#    リクエストはまだ一切していない)
feed_id=$(curl -sf -X POST "$HOST/api/v1/feeds" \
  -H 'Content-Type: application/json' \
  -d "{\"url\": \"$FIXTURE_URL\"}" | jq -r '.feed.id')
echo "enrich_e2e: registered feed_id=$feed_id"

# 2. guid で自分の記事を引く(feed-dedupe.xml が pubDate 2030年で一覧の先頭を占有する
#    ため、「一覧の先頭」には頼れない — 固定 guid での直接検索が唯一の決定的な方法)
article_id=$(curl -sf "$HOST/api/v1/articles?limit=200" \
  | jq -r '.articles[] | select(.guid=="urn:moka-e2e-enrich:1") | .id')
if [ -z "$article_id" ]; then
  echo "enrich_e2e: could not find the registered article by guid" >&2
  exit 1
fi
echo "enrich_e2e: article_id=$article_id"

# 3. enrich.Scheduler が自律的に summary/tags を付けるまでポーリングする
#    (人間・API起点の POST は一切無し)
hurl --test --jobs 1 \
  --variable host="$HOST" \
  --variable article_id="$article_id" \
  e2e/hurl/core/enrich_poll.hurl
