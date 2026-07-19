# パスキー認証 API(M2 / Plecto Phase 2: /api/v1/auth/* — ADR00021)
# 前提: compose.yaml で SESSION_HMAC_KEY_FILE が配線済み + secrets/session_hmac_key.txt が
# 存在すること(未配線なら全エンドポイントが 503 — その分岐は unit テストの領分)。
# WebAuthn の儀式完走(attestation 生成)は Hurl では出来ないため、ここで見るのは
# fresh DB のブートストラップ分岐: 未登録 status / login 404 / register/begin 200 /
# 不正 attestation 400 / challenge の一回性。register/begin の 409(登録済みでの再登録拒否)は
# 儀式完走が前提なので unit テスト(httpapi/auth_test.go)が守る。
# フィードにも記事にも触れないので他シナリオとの順序依存は無い(フレッシュ DB 前提のみ)。
# 変数: host(例 http://localhost:8080)

# 1. health gate — moka-core の起動を待つ
GET {{host}}/healthz
[Options]
retry: 10
retry-interval: 2000
HTTP 200

# 2. fresh DB は未登録 — web はこれでブートストラップ画面(パスキーを作る)を出す
GET {{host}}/api/v1/auth/status
HTTP 200
[Asserts]
jsonpath "$.registered" == false

# 3. 未登録での login/begin は 404(ブートストラップ前にログインは無い)
POST {{host}}/api/v1/auth/login/begin
HTTP 404
[Asserts]
jsonpath "$.error" == "no passkey registered"

# 3b. login/finish も未登録 404(儀式の有無より先に登録有無で断る)
POST {{host}}/api/v1/auth/login/finish
{}
HTTP 404
[Asserts]
jsonpath "$.error" == "no passkey registered"

# 4. register/begin — fresh DB では 200 で CredentialCreation(単一ユーザー固定の RP/user)。
# 409 になるのはパスキー登録済みでブートストラップが閉じた後のみ(fresh DB では起きない)
POST {{host}}/api/v1/auth/register/begin
HTTP 200
[Asserts]
jsonpath "$.publicKey.challenge" exists
jsonpath "$.publicKey.rp.id" == "localhost"
jsonpath "$.publicKey.rp.name" == "moka"
jsonpath "$.publicKey.user.name" == "owner"

# 5. 不正な attestation での register/finish は 400(challenge はこの失敗で消費される)
POST {{host}}/api/v1/auth/register/finish
{"nonsense": true}
HTTP 400
[Asserts]
jsonpath "$.error" == "invalid webauthn response"

# 5b. challenge は一回で消費済み — begin 無しの再 finish は 400(no pending ceremony)
POST {{host}}/api/v1/auth/register/finish
{"nonsense": true}
HTTP 400
[Asserts]
jsonpath "$.error" == "no pending ceremony"

# 6. 失敗した儀式では何も登録されない(fail-closed)
GET {{host}}/api/v1/auth/status
HTTP 200
[Asserts]
jsonpath "$.registered" == false
