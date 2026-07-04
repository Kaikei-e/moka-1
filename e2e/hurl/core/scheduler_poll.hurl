# バックグラウンドスケジューラが自律的に再取得することの検証(受信側)。
# scheduler_e2e.sh が「フィード登録 → fetch_interval_seconds を短縮 → 配信内容を差し替え」まで
# 済ませた後にこのファイルを実行する。人間・API 起点の操作は一切無い状態で、記事が
# 自然に増えることだけを確認する。
# 変数: host, new_guid(例 urn:moka-e2e-sched:2)

GET {{host}}/api/v1/articles?limit=50
[Options]
retry: 20
retry-interval: 1000
HTTP 200
[Asserts]
jsonpath "$.articles[*].guid" contains "{{new_guid}}"
