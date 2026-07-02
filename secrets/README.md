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
