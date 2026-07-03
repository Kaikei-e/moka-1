package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSecret(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "postgres_password")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestBuildDSN(t *testing.T) {
	t.Parallel()

	const base = "postgres://moka@db:5432/moka?sslmode=disable"

	t.Run("injects password from secret file", func(t *testing.T) {
		t.Parallel()

		dsn, err := BuildDSN(base, writeSecret(t, "s3cret\n"))
		require.NoError(t, err)
		assert.Equal(t, "postgres://moka:s3cret@db:5432/moka?sslmode=disable", dsn,
			"改行は落とし、クエリパラメータは保持する")
	})

	t.Run("escapes special characters in password", func(t *testing.T) {
		t.Parallel()

		dsn, err := BuildDSN(base, writeSecret(t, "p@ss/w:rd"))
		require.NoError(t, err)
		// Go の userinfo エンコーダは ':' も %3A にする(URL として等価、pgx は正しく復号する)
		assert.Equal(t, "postgres://moka:p%40ss%2Fw%3Ard@db:5432/moka?sslmode=disable", dsn)
	})

	t.Run("empty password file path passes url through", func(t *testing.T) {
		t.Parallel()

		dsn, err := BuildDSN(base, "")
		require.NoError(t, err)
		assert.Equal(t, base, dsn)
	})

	t.Run("missing secret file is an error", func(t *testing.T) {
		t.Parallel()

		_, err := BuildDSN(base, filepath.Join(t.TempDir(), "nope"))
		assert.Error(t, err)
	})

	t.Run("unparseable database url is an error", func(t *testing.T) {
		t.Parallel()

		_, err := BuildDSN("post gres://\x00bad", writeSecret(t, "pw"))
		assert.Error(t, err)
	})
}
