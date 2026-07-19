# secrets/ — ファイルベース Docker secrets

機微情報は環境変数直書きでなく、ここに置いたファイルを compose の `secrets:` で各コンテナの
`/run/secrets/` にマウントして渡す。**このディレクトリの実ファイルはコミットしない**(gitignore 済み)。

## 初回セットアップ(clone 後に一度だけ)

```bash
openssl rand -base64 24 | tr -d '/+=' > secrets/postgres_password.txt
openssl rand -hex 32 > secrets/session_hmac_key.txt
openssl ecparam -name prime256v1 -genkey -noout | \
  openssl pkcs8 -topk8 -nocrypt -out secrets/plecto_filter_signing_key.pem
```

注意:

- パスワードは migrate ジョブが DSN に埋め込むため、URL に安全な文字のみ(上のコマンドは満たす)。
  改行は末尾の1つまで可(postgres の `_FILE` 系は trailing newline を許容、compose の secret もそのまま渡る)
- `session_hmac_key.txt` は 32 文字以上の `[A-Za-z0-9_-]` のみ(`openssl rand -hex 32` は満たす)。
  レンダリングジョブが manifest の TOML 文字列に埋め込むため、この文字クラスを fail-closed で強制する。
  **鍵の文字列(trim 後)の UTF-8 バイト列がそのまま HMAC-SHA256 鍵**(hex デコードはしない)—
  moka-core(cookie 発行)と plecto フィルタ(検証)で扱いを揃えること(ADR00021)
- `plecto_filter_signing_key.pem` は WASM フィルタ署名用の **dev 鍵**(cosign 既定と同じ
  ECDSA P-256)。公開鍵は `plecto-filters-build` ジョブが導出して Plecto の `[trust]` に渡すので
  手作業は不要。ローテーションは鍵を作り直して build ジョブ再実行 + **plecto 再起動**
  (`[trust]` の変更は SIGHUP リロード不可)

| ファイル | 用途 | 参照サービス |
|---|---|---|
| `postgres_password.txt` | PostgreSQL の moka ユーザーのパスワード | db / migrate / moka-core |
| `session_hmac_key.txt` | セッション署名 cookie の共有シークレット(ADR00021) | moka-core(発行)/ plecto-manifest-render(検証側へ注入) |
| `plecto_filter_signing_key.pem` | WASM フィルタの署名鍵(dev、ECDSA P-256) | plecto-filters-build |
| `cloudflare_tunnel_token.txt` | Cloudflare Tunnel のトークン(任意・compose.tunnel.yaml 使用時のみ) | cloudflared |

### Cloudflare Tunnel を使う場合(任意)

1. Cloudflare Zero Trust ダッシュボードでトンネルを作成(トークン方式)
2. 発行されたトークン文字列をそのまま保存: `echo -n "<トークン>" > secrets/cloudflare_tunnel_token.txt`
3. `docker compose -f compose.yaml -f compose.tunnel.yaml up -d --wait`

Public Hostname のルーティングや TLS(No TLS Verify)設定はダッシュボード側で完結させる。
接続先ホスト名などの個人情報はリポジトリに書かない。
