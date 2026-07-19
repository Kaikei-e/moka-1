# enrich.Scheduler(常駐エージェントループの濃縮ステップ、tenets §3.2 step3)が新着記事へ
# 自動でsummary/tagsを付与することの検証(受信側)。enrich_e2e.sh が「フィード登録」まで
# 済ませた後にこのファイルを実行する。POST .../summary や POST .../tags は一切呼ばない —
# それを呼ばずに中身が見える(GET のみで確認できる)こと自体がこのテストの主張。
# 変数: host, article_id

# summary: enrich.Scheduler が自律的に生成するまでポーリングする(手動POSTは無し)
GET {{host}}/api/v1/articles/{{article_id}}/summary
[Options]
retry: 20
retry-interval: 1000
HTTP 200
[Asserts]
jsonpath "$.summary.article_id" == {{article_id}}
jsonpath "$.summary.text" matches /.+/
jsonpath "$.summary.model_meta.model" matches /.+/
jsonpath "$.summary.created_at" isIsoDate

# tags: 同様に enrich.Scheduler が自律的に生成するまでポーリングする
GET {{host}}/api/v1/articles/{{article_id}}/tags
[Options]
retry: 20
retry-interval: 1000
HTTP 200
[Asserts]
jsonpath "$.tags" count >= 1
