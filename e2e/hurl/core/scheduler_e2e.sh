#!/usr/bin/env bash
# バックグラウンドスケジューラ(tenets §3.2 の常駐エージェントループ step1)が
# 人間・API 起点の操作無しに自律的にフィードを再取得することを検証する。
# Hurl の静的アサーションだけでは「DB を直接いじる」「配信内容を差し替える」という
# 手順を表現できないため、シェルでオーケストレーションして最後だけ Hurl(retry付き
# ポーリング)に委ねる。
#
# 前提: e2e/README.md の手順で moka-core / e2e-fixtures / e2e-db が起動済み、フレッシュ DB。
# リポジトリルートから実行すること: bash e2e/hurl/core/scheduler_e2e.sh
set -euo pipefail

HOST="${E2E_HOST:-http://localhost:8080}"
FIXTURE_URL="${SCHEDULER_FIXTURE_URL:-http://e2e-fixtures/feed-scheduler.xml}"
COMPOSE="docker compose -f compose.yaml -f compose.e2e.yaml"

# 1. 配信内容を v1(記事1件)にする
cp e2e/fixtures/feed-scheduler-v1.xml e2e/fixtures/feed-scheduler.xml

# 2. フィードを登録(同期・API起点。ここまではユーザー操作の代替)
feed_id=$(curl -sf -X POST "$HOST/api/v1/feeds" \
  -H 'Content-Type: application/json' \
  -d "{\"url\": \"$FIXTURE_URL\"}" | jq -r '.feed.id')
echo "scheduler_e2e: registered feed_id=$feed_id"

# 3. このフィードだけ取得間隔を短縮する(テスト専用のDB直接操作 — fetch_interval_seconds を
#    API から設定する導線は今回のスコープ外)
$COMPOSE exec -T e2e-db psql -U moka -d moka -c \
  "UPDATE feeds SET fetch_interval_seconds = 2 WHERE id = ${feed_id};"
echo "scheduler_e2e: shortened fetch_interval_seconds to 2s for feed_id=$feed_id"

# 4. 配信内容を v2(記事2件)に差し替える。ETag/Last-Modified が変わるので次回取得は
#    304 にならず新規記事が入る。以後はスケジューラの自律動作のみが頼り
cp e2e/fixtures/feed-scheduler-v2.xml e2e/fixtures/feed-scheduler.xml
echo "scheduler_e2e: swapped upstream fixture to v2, waiting for the scheduler to notice"

# 5. スケジューラが自律的に再取得して新記事が現れるまでポーリングする(人間・API操作は無し)
hurl --test --jobs 1 \
  --variable host="$HOST" \
  --variable new_guid=urn:moka-e2e-sched:2 \
  e2e/hurl/core/scheduler_poll.hurl
