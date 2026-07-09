# 記事要約(M1: POST /summary, /summary/stream)
# 本物の llm は GPU(iGPU Vulkan passthrough)が要り GitHub-hosted runner では起動できないため、
# CI では compose.e2e.yaml の e2e-llm-mock(決定的な OpenAI 互換モック、e2e/mock-llm/mock_llm.py)
# に対して実行する。moka-core → LLM クライアント → DB保存 → API という配線は実コードのまま検証し、
# 実モデルの応答品質だけがモックに置き換わる(推論品質自体は eval/ の管轄)
# 前提: フレッシュ DB(docker compose down -v 後に up)。実行は --jobs 1(DB 依存シナリオ)。
# feeds_and_articles.hurl とは別ファイル = 別スコープの変数なのでフィード登録から自前で行う
# 変数: host(例 http://localhost:8080), fixture_url(例 http://e2e-fixtures/feed.xml)

# 1. health gate — moka-core の起動を待つ
GET {{host}}/healthz
[Options]
retry: 10
retry-interval: 2000
HTTP 200

# 2. フィクスチャフィードを登録(同じ hurl 実行内で feeds_and_articles.hurl が先に
# 登録済みでも冪等に安全。201/200 のどちらでもよい — 登録件数の検証は feeds_and_articles.hurl 側)
POST {{host}}/api/v1/feeds
{"url": "{{fixture_url}}"}
HTTP *

# 3. 要約対象の記事 id を取る
GET {{host}}/api/v1/articles?limit=1
HTTP 200
[Captures]
article_id: jsonpath "$.articles[0].id"

# 4. 要約 — 初回は新規 201。llm へ実際に投げて要約を得る
POST {{host}}/api/v1/articles/{{article_id}}/summary
HTTP 201
[Captures]
summary_text: jsonpath "$.summary.text"
[Asserts]
jsonpath "$.summary.article_id" == {{article_id}}
jsonpath "$.summary.text" matches /.+/
jsonpath "$.summary.model_meta.model" matches /.+/
jsonpath "$.summary.created_at" isIsoDate

# 5. 冪等な再要約 — 200、llm を再度叩かず保存済みの要約をそのまま返す
POST {{host}}/api/v1/articles/{{article_id}}/summary
HTTP 200
[Asserts]
jsonpath "$.summary.text" == "{{summary_text}}"
[Captures]
first_created_at: jsonpath "$.summary.created_at"

# 5b. ?force=true — 既存要約があっても無視して常に新規生成する(読者が品質に満足できず
# 明示的に「やり直す」場合)。article_summaries は INSERT-only なので新しい行が追記される
# (ADR00002)。201・model_meta.regenerated == true・古い行とは違う created_at がその証拠
# (mock-llm はテキスト自体は固定文言を返すため、行が本当に増えたことは 201 と
# created_at の変化で確認する — feeds_and_articles.hurl の冪等確認と対になるシナリオ)
POST {{host}}/api/v1/articles/{{article_id}}/summary?force=true
HTTP 201
[Asserts]
jsonpath "$.summary.article_id" == {{article_id}}
jsonpath "$.summary.model_meta.regenerated" == true
jsonpath "$.summary.created_at" != "{{first_created_at}}"

# 5c. force 無しの通常 POST に戻す — 200 のまま、かつ latest が更新されている
# (5b で追記された新しい行が返る = ORDER BY created_at DESC LIMIT 1 が効いている)
POST {{host}}/api/v1/articles/{{article_id}}/summary
HTTP 200
[Asserts]
jsonpath "$.summary.model_meta.regenerated" == true

# 6. 存在しない記事は 404
POST {{host}}/api/v1/articles/999999/summary
HTTP 404
[Asserts]
jsonpath "$.error" exists

# 7. 数値でない id は 400
POST {{host}}/api/v1/articles/not-a-number/summary
HTTP 400
[Asserts]
jsonpath "$.error" exists

# 8. ストリーミング要約(専用エンドポイント)— 直前のステップで既に要約済みなので
# 新規生成せず(llm を再度叩かない)、SSE で1回の delta に全文を乗せてから done を返す
POST {{host}}/api/v1/articles/{{article_id}}/summary/stream
HTTP 200
[Asserts]
header "Content-Type" == "text/event-stream"
body contains "event: delta"
body contains "event: done"
body contains "{{summary_text}}"
body not contains "event: error"

# 9. 存在しない記事は 404(SSE ヘッダ送出前の通常 JSON エラー)
POST {{host}}/api/v1/articles/999999/summary/stream
HTTP 404
[Asserts]
jsonpath "$.error" exists

# 10. 数値でない id は 400
POST {{host}}/api/v1/articles/not-a-number/summary/stream
HTTP 400
[Asserts]
jsonpath "$.error" exists
