#!/usr/bin/env bash
# 条件付きGETが効かない(=毎回200で同じ内容を返す)フィードを再取得しても、
# articles.id(IDENTITYシーケンス)の欠番が増えないことを検証する回帰テスト。
# nginx は静的ファイルの mtime から ETag を計算するため、内容を変えずに touch するだけで
# 「サーバが conditional GET を無視して 200 を返す」状況を再現できる
# (通常の再登録シナリオは feeds_and_articles.hurl 側で 304 経路を検証済み)。
#
# 前提: e2e/README.md の手順で moka-core / e2e-fixtures / e2e-db が起動済み、フレッシュ DB。
# リポジトリルートから実行すること: bash e2e/hurl/core/dedupe_no_304_e2e.sh
set -euo pipefail

HOST="${E2E_HOST:-http://localhost:8080}"
FIXTURE_URL="${DEDUPE_FIXTURE_URL:-http://e2e-fixtures/feed-dedupe.xml}"

# 1. 配信内容を v1(記事2件)にする
cp e2e/fixtures/feed-dedupe-v1.xml e2e/fixtures/feed-dedupe.xml

# 2. 初回登録 — 2件挿入される
resp=$(curl -sf -X POST "$HOST/api/v1/feeds" -H 'Content-Type: application/json' -d "{\"url\": \"$FIXTURE_URL\"}")
inserted=$(jq -r '.inserted_articles' <<<"$resp")
if [ "$inserted" != "2" ]; then
  echo "dedupe_no_304_e2e: expected 2 inserted on first register, got $inserted" >&2
  exit 1
fi

# 3. 挿入された2件の id を取得(pubDate を2030年にしてあるので一覧の先頭に来る)
ids=$(curl -sf "$HOST/api/v1/articles?limit=2" | jq -r '.articles[].id')
id1=$(sed -n '1p' <<<"$ids")
id2=$(sed -n '2p' <<<"$ids")
max_id=$(( id1 > id2 ? id1 : id2 ))
echo "dedupe_no_304_e2e: first register inserted ids ${id1},${id2} (max=${max_id})"

# 4. 内容は変えずに mtime だけ更新 → nginx の ETag が変わり、条件付きGETが効かず 200 が返る
touch e2e/fixtures/feed-dedupe.xml

# 5. 同じ内容で再登録 — 200 かつ inserted_articles == 0
#    (事前チェックが無ければ ON CONFLICT で捨てられる2件分もシーケンスを消費してしまう)
status_and_body=$(curl -sf -w '\n%{http_code}' -X POST "$HOST/api/v1/feeds" -H 'Content-Type: application/json' -d "{\"url\": \"$FIXTURE_URL\"}")
status="${status_and_body##*$'\n'}"
body="${status_and_body%$'\n'*}"
if [ "$status" != "200" ]; then
  echo "dedupe_no_304_e2e: expected 200 on no-op re-register, got $status" >&2
  exit 1
fi
inserted=$(jq -r '.inserted_articles' <<<"$body")
if [ "$inserted" != "0" ]; then
  echo "dedupe_no_304_e2e: expected 0 inserted on no-op re-register, got $inserted (sequence-burn regression)" >&2
  exit 1
fi

# 6. 本当に新しい記事を1件追加した配信に差し替えて再取得 — 次の新規記事の id が
#    max_id+1 に連続していることを検証する(事前チェックが無ければ手順5で2つ欠番が
#    生まれ、max_id+3 になっていたはず)
cp e2e/fixtures/feed-dedupe-v2.xml e2e/fixtures/feed-dedupe.xml
touch e2e/fixtures/feed-dedupe.xml
resp=$(curl -sf -X POST "$HOST/api/v1/feeds" -H 'Content-Type: application/json' -d "{\"url\": \"$FIXTURE_URL\"}")
inserted=$(jq -r '.inserted_articles' <<<"$resp")
if [ "$inserted" != "1" ]; then
  echo "dedupe_no_304_e2e: expected 1 inserted after adding a genuinely new item, got $inserted" >&2
  exit 1
fi

expected_new_id=$(( max_id + 1 ))
hurl --test --jobs 1 \
  --variable host="$HOST" \
  --variable new_guid=urn:moka-e2e-dedupe:3 \
  --variable expected_id="$expected_new_id" \
  e2e/hurl/core/dedupe_no_304.hurl

echo "dedupe_no_304_e2e: OK — no-op re-register consumed no id gap (new article id == max_id+1 == ${expected_new_id})"
