// Package store は pgx による永続化アダプタ。手書き SQL が真実(ORM 無し)。
// マイグレーションは Atlas の管轄で、このパッケージはスキーマに関与しない(ADR00001)。
package store

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BuildDSN は DATABASE_URL に Docker secret のパスワードを合成する
// (compose.yaml の「store 層が POSTGRES_PASSWORD_FILE を読んで DSN に合成する契約」)。
// passwordFile が空なら databaseURL をそのまま返す(ローカル開発用)。
func BuildDSN(databaseURL, passwordFile string) (string, error) {
	if passwordFile == "" {
		return databaseURL, nil
	}

	raw, err := os.ReadFile(passwordFile)
	if err != nil {
		return "", fmt.Errorf("read password file %s: %w", passwordFile, err)
	}
	password := strings.TrimSpace(string(raw))

	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("parse database url: %w", err)
	}
	username := ""
	if u.User != nil {
		username = u.User.Username()
	}
	u.User = url.UserPassword(username, password)
	return u.String(), nil
}

// NewPool は DSN を合成して接続プールを作り、疎通を確認して返す。
// Close は呼び出し側(composition root)の責務。
func NewPool(ctx context.Context, databaseURL, passwordFile string) (*pgxpool.Pool, error) {
	dsn, err := BuildDSN(databaseURL, passwordFile)
	if err != nil {
		return nil, fmt.Errorf("build dsn: %w", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}

// Store は feeds / articles / feed_fetches の永続化。
// feed.Store と httpapi.ArticleLister を満たす(main で注入)。
type Store struct {
	pool *pgxpool.Pool
}

// New はプールを包んだ Store を返す。
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}
