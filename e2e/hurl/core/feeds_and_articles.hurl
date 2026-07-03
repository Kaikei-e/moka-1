# RSS フィード登録 → 記事一覧(M0: feed registered → articles readable)
# 前提: フレッシュ DB(docker compose down -v 後に up)。実行は --jobs 1(DB 依存シナリオ)
# 変数: host(例 http://localhost:8080), fixture_url(例 http://e2e-fixtures/feed.xml)

# 1. health gate — moka-core の起動を待つ
GET {{host}}/healthz
[Options]
retry: 10
retry-interval: 2000
HTTP 200

# 2. フィクスチャフィードを登録 → 検証・取得・パース・保存まで同期実行
POST {{host}}/api/v1/feeds
{"url": "{{fixture_url}}"}
HTTP 201
[Captures]
feed_id: jsonpath "$.feed.id"
[Asserts]
jsonpath "$.feed.url" == "{{fixture_url}}"
jsonpath "$.feed.title" == "Moka E2E Fixture"
jsonpath "$.feed.created_at" isIsoDate
jsonpath "$.inserted_articles" == 3

# 3. 冪等な再登録 → 200、条件付き GET(nginx ネイティブ 304)で挿入 0
POST {{host}}/api/v1/feeds
{"url": "{{fixture_url}}"}
HTTP 200
[Asserts]
jsonpath "$.feed.id" == {{feed_id}}
jsonpath "$.inserted_articles" == 0

# 4. 不正スキームは 400
POST {{host}}/api/v1/feeds
{"url": "ftp://example.com/feed"}
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 5. 記事一覧 — 新しい順(published_at DESC)、カーソルベースページング(offset は持たない)
GET {{host}}/api/v1/articles?limit=2
HTTP 200
[Captures]
article_id: jsonpath "$.articles[0].id"
next_cursor: jsonpath "$.next_cursor"
[Asserts]
jsonpath "$.articles" count == 2
jsonpath "$.articles[0].guid" == "urn:moka-e2e:3"
jsonpath "$.articles[0].title" == "Third article"
jsonpath "$.articles[0].published_at" isIsoDate
jsonpath "$.articles[1].guid" == "urn:moka-e2e:2"
jsonpath "$.next_cursor" isString

# 5b. カーソルで次ページ — 重複も取りこぼしも無く、終端で next_cursor は null
GET {{host}}/api/v1/articles?limit=2&cursor={{next_cursor}}
HTTP 200
[Asserts]
jsonpath "$.articles" count == 1
jsonpath "$.articles[0].guid" == "urn:moka-e2e:1"
jsonpath "$.next_cursor" == null

# 5c. 壊れたカーソルは 400
GET {{host}}/api/v1/articles?cursor=not-a-cursor
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 6. 登録済みフィード一覧 — フィード管理画面のデータ源
GET {{host}}/api/v1/feeds
HTTP 200
[Asserts]
jsonpath "$.feeds" count == 1
jsonpath "$.feeds[0].url" == "{{fixture_url}}"
jsonpath "$.feeds[0].title" == "Moka E2E Fixture"
jsonpath "$.feeds[0].created_at" isIsoDate

# 7. 記事単体 — 読書ビューのデータ源
GET {{host}}/api/v1/articles/{{article_id}}
HTTP 200
[Asserts]
jsonpath "$.article.id" == {{article_id}}
jsonpath "$.article.guid" == "urn:moka-e2e:3"
jsonpath "$.article.title" == "Third article"
jsonpath "$.article.content" exists

# 8. 存在しない記事は 404
GET {{host}}/api/v1/articles/999999
HTTP 404
[Asserts]
jsonpath "$.error" exists

# 9. 数値でない id は 400
GET {{host}}/api/v1/articles/not-a-number
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 10. 全文取り寄せ — 初回は新規 201。フィクスチャ記事ページから抽出され、
# RSS 由来の概要(description)より詳しい本文が返る
POST {{host}}/api/v1/articles/{{article_id}}/fulltext
HTTP 201
[Captures]
fulltext_text: jsonpath "$.fulltext.text"
[Asserts]
jsonpath "$.fulltext.article_id" == {{article_id}}
jsonpath "$.fulltext.text" contains "ninety-nine"
jsonpath "$.fulltext.fetched_at" isIsoDate

# 11. 冪等な再取り寄せ — 200、外部サイトを再度叩かず保存済みの全文をそのまま返す
POST {{host}}/api/v1/articles/{{article_id}}/fulltext
HTTP 200
[Asserts]
jsonpath "$.fulltext.text" == "{{fulltext_text}}"

# 12. 存在しない記事は 404
POST {{host}}/api/v1/articles/999999/fulltext
HTTP 404
[Asserts]
jsonpath "$.error" exists

# 13. 数値でない id は 400
POST {{host}}/api/v1/articles/not-a-number/fulltext
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 14. 要約 — 初回は新規 201。llm(Qwen3.5-4B, ADR00007)へ実際に投げて要約を得る
POST {{host}}/api/v1/articles/{{article_id}}/summary
HTTP 201
[Captures]
summary_text: jsonpath "$.summary.text"
[Asserts]
jsonpath "$.summary.article_id" == {{article_id}}
jsonpath "$.summary.text" matches /.+/
jsonpath "$.summary.model_meta.model" matches /.+/
jsonpath "$.summary.created_at" isIsoDate

# 15. 冪等な再要約 — 200、llm を再度叩かず保存済みの要約をそのまま返す
POST {{host}}/api/v1/articles/{{article_id}}/summary
HTTP 200
[Asserts]
jsonpath "$.summary.text" == "{{summary_text}}"

# 16. 存在しない記事は 404
POST {{host}}/api/v1/articles/999999/summary
HTTP 404
[Asserts]
jsonpath "$.error" exists

# 17. 数値でない id は 400
POST {{host}}/api/v1/articles/not-a-number/summary
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 18. ストリーミング要約(専用エンドポイント)— 直前のステップで既に要約済みなので
# 新規生成せず(llm を再度叩かない)、SSE で1回の delta に全文を乗せてから done を返す
POST {{host}}/api/v1/articles/{{article_id}}/summary/stream
HTTP 200
[Asserts]
header "Content-Type" == "text/event-stream"
body contains "event: delta"
body contains "event: done"
body contains "{{summary_text}}"
body not contains "event: error"

# 19. 存在しない記事は 404(SSE ヘッダ送出前の通常 JSON エラー)
POST {{host}}/api/v1/articles/999999/summary/stream
HTTP 404
[Asserts]
jsonpath "$.error" exists

# 20. 数値でない id は 400
POST {{host}}/api/v1/articles/not-a-number/summary/stream
HTTP 400
[Asserts]
jsonpath "$.error" exists
