# 問い返し Q&A の文脈検索クエリ配線(RAG 精度改善 — 変更1: タイトル連結)。
# qa_context_relevance_e2e.sh が feed-qa-context.xml(同一タイトル・別トピックの2記事)を
# 登録した後にこのファイルを実行する。トピック語を含まない一般要約質問(qa.hurl と同じ文言)
# を対象記事(記事1)へ投げると、対象記事のタイトルが検索クエリへ連結されて初めて、
# 同タイトルを持つもう一方の記事(記事2、内容は無関係)が文脈選定のヒット集合に
# 入ってくる — これがクエリ構成の変更(質問文のみ→タイトル+質問文)を外部から観測できる
# 唯一の配線レベルの契約。意味的な関連性そのものはモック埋め込みでは判定できない
# (e2e/README.md の e2e-llm-mock 節参照)。
# 変数: host(例 http://localhost:8080), target_article_id(記事1の id),
#       distractor_marker(記事2の URL に含まれる一意な文字列、例 qactx-2)

# 1. health gate — moka-core の起動を待つ
GET {{host}}/healthz
[Options]
retry: 10
retry-interval: 2000
HTTP 200

# 2. トピック語を含まない一般要約質問(qa.hurl と同文言)。対象記事(記事1)のタイトルが
# クエリに連結されて初めて、同タイトルを持つ記事2(内容は無関係)が sources に現れる。
# 質問文単独では記事1・記事2どちらの英字タイトルとも pg_trgm 類似度が付かない
# (質問は日本語、フィクスチャ群は英字タイトルのみ)ため、この観測は「クエリにタイトルが
# 乗っているかどうか」だけに依存する
POST {{host}}/api/v1/articles/{{target_article_id}}/qa
{"question": "この記事の要点を教えてください"}
HTTP 200
[Asserts]
header "Content-Type" == "text/event-stream"
body contains "event: sources"
body contains "event: done"
body not contains "event: error"
body contains "{{distractor_marker}}"
