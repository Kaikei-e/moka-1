# published_at が NULL の記事のフォールバック順(COALESCE(published_at, created_at))
# 前提: フレッシュ DB、かつ feeds_and_articles.hurl(25記事)/ summarize.hurl の後に実行すること
# (先頭記事を limit=1 で拾う summarize.hurl の前提を崩さないため)。--jobs 1。
# 変数: host, null_pubdate_fixture_url(例 http://e2e-fixtures/feed-null-pubdate.xml)
#
# フィクスチャは3記事: item1 は pubDate 無し(published_at NULL、fallback で「今」扱い
# = 全記事中で最新になるはず)。item2(2026-04-01)/item3(2026-03-01)は feed.xml 側の
# 日付レンジ(2026-06-07〜07-01)より確実に古い過去日付にしてあり、他ファイルの記事と
# 混線しない。

# 1. health gate
GET {{host}}/healthz
[Options]
retry: 10
retry-interval: 2000
HTTP 200

# 2. フィクスチャフィードを登録
POST {{host}}/api/v1/feeds
{"url": "{{null_pubdate_fixture_url}}"}
HTTP 201
[Asserts]
jsonpath "$.inserted_articles" == 3

# 3. 記事一覧の先頭(limit=1, カーソル無し)— published_at が NULL の記事が
# 「最下部に沈む」のではなく、fallback(created_at≈登録時刻)で全記事中の最新として
# 先頭に来ることを確認する(修正前は NULLS LAST で最後尾に落ちるため、ここで失敗する)
GET {{host}}/api/v1/articles?limit=1
HTTP 200
[Captures]
cursor_1: jsonpath "$.next_cursor"
[Asserts]
jsonpath "$.articles[0].guid" == "urn:moka-e2e-null:1"
jsonpath "$.articles[0].published_at" == null

# 4. 続く25件(feeds_and_articles.hurl のフィクスチャ、2026-07-01〜06-07)を丸ごと1ページで
# 消費する。published_at が NULL だった item1 が重複して混ざり込まないことを確認する
GET {{host}}/api/v1/articles?limit=25&cursor={{cursor_1}}
HTTP 200
[Captures]
cursor_2: jsonpath "$.next_cursor"
[Asserts]
jsonpath "$.articles" count == 25
jsonpath "$.articles[0].guid" == "urn:moka-e2e:3"
jsonpath "$.articles[24].guid" == "urn:moka-e2e:25"

# 5. 残りの2件(item2, item3、日付順)を最後のページで取得。limit を残数(2件)より
# 大きくして「満杯ページ」を避け、重複なく next_cursor が無い(終端)ことを確認する
GET {{host}}/api/v1/articles?limit=5&cursor={{cursor_2}}
HTTP 200
[Asserts]
jsonpath "$.articles" count == 2
jsonpath "$.articles[0].guid" == "urn:moka-e2e-null:2"
jsonpath "$.articles[1].guid" == "urn:moka-e2e-null:3"
jsonpath "$.next_cursor" == null
