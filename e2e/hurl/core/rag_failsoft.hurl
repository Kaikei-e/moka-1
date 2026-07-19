# llm 完全停止時のフェイルソフト(M2: 検索はテキスト単独 200 / Q&A は SSE error)
# 実行は rag_failsoft_e2e.sh 経由(e2e-llm-mock を止めてから走らせ、終了時に必ず再開する)。
# 検索(増強)は llm が死んでもテキスト側単独に縮退して 200 を返し続け(tenets §2-6)、
# Q&A は SSE ヘッダ送出後のため HTTP 200 のまま error イベントで静かに失敗する。
# 変数: host(例 http://localhost:8080), article_id(存在する記事の id)

# 1. health gate — moka-core 自体は llm 停止に関係なく生きている(コアパスは無傷)
GET {{host}}/healthz
[Options]
retry: 10
retry-interval: 2000
HTTP 200

# 2. llm 停止時も検索は 200 — pg_trgm テキスト側単独への縮退(RRF のベクトル側は空)
GET {{host}}/api/v1/search?q=Third%20article
HTTP 200
[Asserts]
jsonpath "$.items" count >= 1
jsonpath "$.items[0].title" == "Third article"

# 2b. ヒットなしは 200 の空配列(null でなく [] — 一覧 API と同じ契約)。
# 埋め込みが効いている通常時はベクトル近傍が必ず何か返す(top-k に閾値は無い)ため、
# この契約はクエリ埋め込みが縮退で空になる llm 停止時にだけ決定的に観測できる
# (search.hurl #6 の注記と対)
GET {{host}}/api/v1/search?q=qqqzzzxxxvvv
HTTP 200
[Asserts]
jsonpath "$.items" count == 0

# 3. Q&A は sources(文脈選定はテキスト検索で成立)まで届いた後、回答生成で llm に
# 到達できず error イベントになる。done は出ない。message はドメイン sentinel の写像
POST {{host}}/api/v1/articles/{{article_id}}/qa
{"question": "llm が居ないとどうなりますか"}
HTTP 200
[Asserts]
header "Content-Type" == "text/event-stream"
body contains "event: sources"
body contains "event: error"
body contains "llm unavailable"
body not contains "event: done"
