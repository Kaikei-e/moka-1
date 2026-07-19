# 問い返し Q&A(M2: POST /api/v1/articles/{id}/qa — SSE: sources → delta → done)
# CI では e2e-llm-mock(決定的な OpenAI 互換モック)がストリーミング補完を返す。
# moka-core → ハイブリッド検索(文脈選定)→ LLM クライアント → SSE → DB 保存という配線は
# 実コードのまま検証し、回答品質はアサートしない(eval/ の管轄)。
# llm 停止時の error イベント(フェイルソフト)は rag_failsoft_e2e.sh 側で検証する。
# 前提: フレッシュ DB、--jobs 1。search.hurl の後・articles_null_published_at.hurl の前に置く
# (「limit=1 で先頭記事」の前提を崩す独自フィード登録シナリオより先に走らせる)
# 変数: host(例 http://localhost:8080), fixture_url(例 http://e2e-fixtures/feed.xml)

# 1. health gate — moka-core の起動を待つ
GET {{host}}/healthz
[Options]
retry: 10
retry-interval: 2000
HTTP 200

# 2. フィクスチャフィードを登録(冪等 — 登録件数の検証は feeds_and_articles.hurl 側)
POST {{host}}/api/v1/feeds
{"url": "{{fixture_url}}"}
HTTP *

# 3. 質問対象の記事 id を取る
GET {{host}}/api/v1/articles?limit=1
HTTP 200
[Captures]
article_id: jsonpath "$.articles[0].id"

# 4. 正常系 — SSE のイベント順: sources(文脈記事、回答生成の前)→ delta(回答断片)→
# done(question_id / answer_id)。error は出ない。文脈検索はテキスト側単独に縮退しても
# 回答は成立する(検索は増強であって回答の前提条件ではない)
POST {{host}}/api/v1/articles/{{article_id}}/qa
{"question": "この記事の要点を教えてください"}
HTTP 200
[Asserts]
header "Content-Type" == "text/event-stream"
body contains "event: sources"
body contains "event: delta"
body contains "event: done"
body contains "question_id"
body contains "answer_id"
body not contains "event: error"

# 5. question 欠落は 400(SSE ヘッダ送出前の通常 JSON エラー)
POST {{host}}/api/v1/articles/{{article_id}}/qa
{}
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 5b. 空白のみの question も 400
POST {{host}}/api/v1/articles/{{article_id}}/qa
{"question": "   "}
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 6. 存在しない記事は 404
POST {{host}}/api/v1/articles/999999/qa
{"question": "この記事の要点を教えてください"}
HTTP 404
[Asserts]
jsonpath "$.error" exists

# 7. 数値でない id は 400
POST {{host}}/api/v1/articles/not-a-number/qa
{"question": "この記事の要点を教えてください"}
HTTP 400
[Asserts]
jsonpath "$.error" exists
