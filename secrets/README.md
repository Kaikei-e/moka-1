# secrets/ — ファイルベース Docker secrets

機微情報は環境変数直書きでなく、ここに置いたファイルを compose の `secrets:` で各コンテナの
`/run/secrets/` にマウントして渡す。**このディレクトリの実ファイルはコミットしない**(gitignore 済み)。

## 初回セットアップ(clone 後に一度だけ)

```bash
openssl rand -base64 24 | tr -d '/+=' > secrets/postgres_password.txt
```

注意: パスワードは migrate ジョブが DSN に埋め込むため、URL に安全な文字のみ(上のコマンドは満たす)。
改行は末尾の1つまで可(postgres の `_FILE` 系は trailing newline を許容、compose の secret もそのまま渡る)。

| ファイル | 用途 | 参照サービス |
|---|---|---|
| `postgres_password.txt` | PostgreSQL の moka ユーザーのパスワード | db / migrate / moka-core |
| `cloudflare_tunnel_token.txt` | Cloudflare Tunnel のトークン(任意・compose.tunnel.yaml 使用時のみ) | cloudflared |

### Cloudflare Tunnel を使う場合(任意)

1. Cloudflare Zero Trust ダッシュボードでトンネルを作成(トークン方式)
2. 発行されたトークン文字列をそのまま保存: `echo -n "<トークン>" > secrets/cloudflare_tunnel_token.txt`
3. `docker compose -f compose.yaml -f compose.tunnel.yaml up -d --wait`

Public Hostname のルーティングや TLS(No TLS Verify)設定はダッシュボード側で完結させる。
接続先ホスト名などの個人情報はリポジトリに書かない。
