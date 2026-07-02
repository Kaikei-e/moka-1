# Atlas 設定(ADR00001)。適用は compose の one-shot `migrate` ジョブが行う。
# 開発フロー(ホストから):
#   atlas migrate diff <name> --env local     # schema.sql との差分から versioned SQL を生成
#   atlas migrate lint --env local --latest 1 # 破壊的変更の検査
#   atlas migrate hash --env local            # 手書き SQL を足した場合の atlas.sum 更新

env "local" {
  # compose の db(127.0.0.1:5432 公開)。パスワードは .env と揃える
  url = getenv("DATABASE_URL")
  src = "file://schema.sql"

  # diff/lint 用の使い捨て dev-database。pgvector 拡張が要るので素の postgres ではなく pgvector イメージ
  dev = "docker+postgres://pgvector/pgvector:0.8.4-pg18-trixie/dev"

  migration {
    dir = "file://migrations"
  }
}
