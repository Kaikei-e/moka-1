# ハイブリッド検索(M2: GET /api/v1/search — pg_trgm + pgvector の RRF 融合、ADR00022)
# e2e-llm-mock は /embeddings(決定的な 3-gram feature hashing、1024次元)も実装しており、
# enrich.Scheduler が記事を自動で埋め込む。ここではテキスト側(pg_trgm)の契約に加えて、
# ベクトル側(pgvector 近傍)が RRF に効くケースも検証する(#6 — 埋め込みの完了を retry で待つ)。
# llm 停止時のテキスト単独縮退 + ヒットなし空配列 + Q&A error は rag_failsoft_e2e.sh 側で検証する。
# 前提: フレッシュ DB、--jobs 1。summarize.hurl の後・articles_null_published_at.hurl の前に置く
# (独自フィードを追加する後続シナリオが検索ヒットに混ざらないうちに走らせる)
# 変数: host(例 http://localhost:8080), fixture_url(例 http://e2e-fixtures/feed.xml)

# 1. health gate — moka-core の起動を待つ
GET {{host}}/healthz
[Options]
retry: 10
retry-interval: 2000
HTTP 200

# 2. フィクスチャフィードを登録(冪等 — 同じ hurl 実行内で登録済みなら 200 / 挿入 0)
POST {{host}}/api/v1/feeds
{"url": "{{fixture_url}}"}
HTTP *

# 3. 空クエリは 400
GET {{host}}/api/v1/search
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 3b. 空白のみのクエリも 400(TrimSpace 後に空)
GET {{host}}/api/v1/search?q=%20%20
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 4. 不正な limit は 400
GET {{host}}/api/v1/search?q=article&limit=0
HTTP 400
[Asserts]
jsonpath "$.error" exists

GET {{host}}/api/v1/search?q=article&limit=not-a-number
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 5. ヒットあり — タイトル完全一致が先頭。封筒は items、記事表現は一覧 API と同じ
# (feed.Article 埋め込み)+ RRF 融合スコア(score)。埋め込みの有無(enrich.Scheduler の
# 進み具合)に依らずテキスト側 rank 1 の「Third article」が先頭に来る(ベクトル側でも
# 3-gram 重なり最大 = 同記事が上位に来るため、RRF 融合後も順位は崩れない)。
# 埋め込みが部分的にしか済んでいない一瞬(Third 未埋め込みで他記事だけベクトル側に居る)は
# RRF の同点タイブレークで順位が入れ替わりうるため retry で乗り切る
GET {{host}}/api/v1/search?q=Third%20article
[Options]
retry: 30
retry-interval: 2000
HTTP 200
[Asserts]
jsonpath "$.items" count >= 1
jsonpath "$.items[0].title" == "Third article"
jsonpath "$.items[0].guid" == "urn:moka-e2e:3"
jsonpath "$.items[0].id" isInteger
jsonpath "$.items[0].published_at" isIsoDate
jsonpath "$.items[0].score" isFloat

# 5b. limit が効く(上限まで絞られる)。retry は #5 と同じ理由
GET {{host}}/api/v1/search?q=Third%20article&limit=1
[Options]
retry: 30
retry-interval: 2000
HTTP 200
[Asserts]
jsonpath "$.items" count == 1
jsonpath "$.items[0].title" == "Third article"

# 6. ベクトル側が効く — テキスト側(pg_trgm)が 1 件も返さない無意味クエリでも、
# 埋め込み済み記事が cosine 近傍として返る = ヒットは純粋にベクトル側(article_embeddings)由来。
# enrich.Scheduler の自動埋め込み(MOKA_ENRICH_TICK_SECONDS=2)が最初の記事を
# 埋め込み終わるまで retry で待つ。
# 注: この挙動により「ヒットなし = 空配列」は埋め込みが存在する限り観測できない
# (top-k 近傍検索に閾値は無い)。空配列の契約は llm 停止時(ベクトル側が縮退で空)の
# rag_failsoft.hurl 側で検証する
GET {{host}}/api/v1/search?q=qqqzzzxxxvvv
[Options]
retry: 60
retry-interval: 2000
HTTP 200
[Asserts]
jsonpath "$.items" count >= 1
jsonpath "$.items[0].id" isInteger
jsonpath "$.items[0].score" isFloat

# 6b. ベクトル側が生きた後も、テキスト×ベクトルの RRF 融合で「Third article」が先頭のまま。
# #6 は「最初の 1 記事」の埋め込みしか待っていない(batch 20/tick なので全 25 記事は
# 複数 tick かかる)。Third 未埋め込みの窓では #5 と同じ同点タイブレークで別記事が先頭に
# 来うるため、ここも retry で全記事の埋め込み完了まで乗り切る
GET {{host}}/api/v1/search?q=Third%20article
[Options]
retry: 30
retry-interval: 2000
HTTP 200
[Asserts]
jsonpath "$.items" count >= 1
jsonpath "$.items[0].title" == "Third article"
