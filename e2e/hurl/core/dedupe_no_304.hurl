# dedupe_no_304_e2e.sh から呼ばれる最終アサーション。
# 条件付きGETを迂回した無駄な再取得の後でも、本当に新しい記事のIDが連番のまま
# (欠番が増えていない)ことを確認する。
GET {{host}}/api/v1/articles?limit=1
HTTP 200
[Asserts]
jsonpath "$.articles[0].guid" == "{{new_guid}}"
jsonpath "$.articles[0].id" == {{expected_id}}
