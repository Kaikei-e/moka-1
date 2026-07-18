# 既読マーク(POST /api/v1/articles/{id}/read)とフィード削除(DELETE /api/v1/feeds/{id})
# 前提: フレッシュ DB、かつ feeds_and_articles.hurl(25記事)/ summarize.hurl /
# articles_null_published_at.hurl(3記事)の後に実行すること — 一覧の先頭2件が
# urn:moka-e2e-null:1 / urn:moka-e2e:3 である前提を使う。--jobs 1。
# 末尾で null-pubdate フィードを削除するため、このファイルは hurl 群の最後に置く
# (後続の dedupe / scheduler スクリプトは独自フィードだけを使うので影響しない)。
# 変数: host, fixture_url, null_pubdate_fixture_url

# 1. health gate — moka-core の起動を待つ
GET {{host}}/healthz
[Options]
retry: 10
retry-interval: 2000
HTTP 200

# 2. 一覧の先頭2件: null フィードの item1(fallback で全記事中最新)と本体フィクスチャの
# 最新記事。未読 = read が false(article_reads に行が無い)、feed_title は所属フィードの title
GET {{host}}/api/v1/articles?limit=2
HTTP 200
[Captures]
null_article_id: jsonpath "$.articles[0].id"
read_article_id: jsonpath "$.articles[1].id"
[Asserts]
jsonpath "$.articles[0].guid" == "urn:moka-e2e-null:1"
jsonpath "$.articles[0].read" == false
jsonpath "$.articles[0].feed_title" == "Moka E2E Null PubDate Fixture"
jsonpath "$.articles[1].guid" == "urn:moka-e2e:3"
jsonpath "$.articles[1].read" == false
jsonpath "$.articles[1].feed_title" == "Moka E2E Fixture"

# 3. 既読マーク — 204(ボディ無し)
POST {{host}}/api/v1/articles/{{read_article_id}}/read
HTTP 204

# 4. 冪等な再マーク — 204 のまま(article_reads に行は増えない)
POST {{host}}/api/v1/articles/{{read_article_id}}/read
HTTP 204

# 5. 一覧に read フラグが現れる。マークしていない記事は false のまま
GET {{host}}/api/v1/articles?limit=2
HTTP 200
[Asserts]
jsonpath "$.articles[0].read" == false
jsonpath "$.articles[1].read" == true

# 6. 記事単体(読書ビュー)でも read / feed_title が返る
GET {{host}}/api/v1/articles/{{read_article_id}}
HTTP 200
[Asserts]
jsonpath "$.article.read" == true
jsonpath "$.article.feed_title" == "Moka E2E Fixture"

# 7. 存在しない記事の既読マークは 404
POST {{host}}/api/v1/articles/999999/read
HTTP 404
[Asserts]
jsonpath "$.error" exists

# 8. 数値でない id は 400
POST {{host}}/api/v1/articles/not-a-number/read
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 9. フィード一覧の先頭 = 最後に登録した null-pubdate フィード(created_at DESC, id DESC)
GET {{host}}/api/v1/feeds
HTTP 200
[Captures]
del_feed_id: jsonpath "$.feeds[0].id"
[Asserts]
jsonpath "$.feeds" count == 2
jsonpath "$.feeds[0].url" == "{{null_pubdate_fixture_url}}"

# 10. フィード削除 — 204。配下の記事・既読・要約は FK の ON DELETE CASCADE が一括削除する
DELETE {{host}}/api/v1/feeds/{{del_feed_id}}
HTTP 204

# 11. 二度目は 404(既に無い)
DELETE {{host}}/api/v1/feeds/{{del_feed_id}}
HTTP 404
[Asserts]
jsonpath "$.error" exists

# 12. 数値でない id は 400
DELETE {{host}}/api/v1/feeds/not-a-number
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 13. フィード一覧から消えている
GET {{host}}/api/v1/feeds
HTTP 200
[Asserts]
jsonpath "$.feeds" count == 1
jsonpath "$.feeds[0].url" == "{{fixture_url}}"

# 14. 配下の記事も CASCADE で消えている
GET {{host}}/api/v1/articles/{{null_article_id}}
HTTP 404
[Asserts]
jsonpath "$.error" exists

# 15. 一覧は本体フィクスチャの25件に戻り、先頭は既読マークした記事のまま
# (別フィードの削除は他フィードの既読の事実を巻き込まない)
GET {{host}}/api/v1/articles?limit=30
HTTP 200
[Asserts]
jsonpath "$.articles" count == 25
jsonpath "$.articles[0].guid" == "urn:moka-e2e:3"
jsonpath "$.articles[0].read" == true
jsonpath "$.next_cursor" == null
