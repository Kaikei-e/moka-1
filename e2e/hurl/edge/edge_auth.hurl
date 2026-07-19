# エッジ(Plecto Phase 2)のセッション認証 + レート制限フィルタ(ADR00021、tenets §3.5)
# 実行は edge_auth_e2e.sh 経由(有効/期限切れ/改竄の署名 cookie を secrets の共有シークレット
# から鋳造して変数で渡す)。自己署名証明書なので hurl 自体を --insecure で走らせる。
# 変数: edge_host(例 https://localhost), valid_cookie, expired_cookie, tampered_cookie
#   (いずれも moka_session cookie の値 — v1.<exp_ms>.<base64url_nopad(HMAC-SHA256)>)

# 1. health gate — /healthz ルートは認証除外(upstream 契約の監視経路)。
# エッジ → moka-web まで生きていることを兼ねて確認する
GET {{edge_host}}/healthz
[Options]
retry: 10
retry-interval: 2000
HTTP 200

# 2. 未認証の text/html GET はログイン画面へ 302(ブラウザのページ遷移の作法)
GET {{edge_host}}/
Accept: text/html,application/xhtml+xml
HTTP 302
[Asserts]
header "Location" contains "/auth/login"

# 3. 未認証の非 html(API 的アクセス)は 401 + WWW-Authenticate(RFC 9110)
GET {{edge_host}}/
Accept: application/json
HTTP 401
[Asserts]
header "WWW-Authenticate" exists

# 4. 改竄 cookie は無効(fail-closed — 署名が合わなければ未認証と同じ)
GET {{edge_host}}/
Accept: application/json
Cookie: moka_session={{tampered_cookie}}
HTTP 401

# 5. 署名は正しいが期限切れの cookie も無効(exp_unix_ms > now の検査)
GET {{edge_host}}/
Accept: application/json
Cookie: moka_session={{expired_cookie}}
HTTP 401

# 6. 有効な署名 cookie は通る — moka-web の SSR ページが返る
GET {{edge_host}}/
Accept: text/html,application/xhtml+xml
Cookie: moka_session={{valid_cookie}}
HTTP 200
[Asserts]
header "Content-Type" contains "text/html"

# 7. /auth はセッション認証の除外パス(未認証者の入口)— cookie 無しで 200
GET {{edge_host}}/auth/login
Accept: text/html,application/xhtml+xml
HTTP 200

# 8. /auth はバケットが厳しい(capacity 10, refill 1/s)— バーストで尽きる。
# ここまでで /auth は 1 回消費済み。素早く 12 回叩けばバケットは必ず空になる。
# per-IP バケットを汚すので、このシナリオはファイル末尾・edge 検証の最後に置く
GET {{edge_host}}/auth/login
[Options]
repeat: 12
HTTP *

# 8b. 尽きた直後の 1 発は 429(Retry-After で補充を伝える)
GET {{edge_host}}/auth/login
HTTP 429
[Asserts]
header "Retry-After" exists

# 9. バケットは経路別 — /auth が 429 でも通常経路(capacity 100)は影響を受けない
GET {{edge_host}}/
Accept: text/html,application/xhtml+xml
Cookie: moka_session={{valid_cookie}}
HTTP 200
