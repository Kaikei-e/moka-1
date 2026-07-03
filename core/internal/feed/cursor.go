package feed

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidCursor は復号できないカーソル。httpapi が 400 へ写像する
var ErrInvalidCursor = errors.New("invalid article cursor")

// ArticleCursor は記事一覧の keyset ページング位置(最後に返した記事の並びキー)。
// 並びは published_at DESC NULLS LAST, id DESC — PublishedAt が nil のカーソルは
// NULLS LAST の末尾領域(published_at IS NULL)を指す。
type ArticleCursor struct {
	PublishedAt *time.Time
	ID          int64
}

// Encode は不透明なカーソル文字列(base64url)を返す。形式は "RFC3339Nano|id"。
// クライアントに中身を契約させない — 復号は DecodeArticleCursor だけが知る。
func (c ArticleCursor) Encode() string {
	ts := ""
	if c.PublishedAt != nil {
		ts = c.PublishedAt.UTC().Format(time.RFC3339Nano)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(ts + "|" + strconv.FormatInt(c.ID, 10)))
}

// DecodeArticleCursor は Encode の逆。壊れた入力はすべて ErrInvalidCursor に畳む
// (内部形式をエラーメッセージで漏らさない)。
func DecodeArticleCursor(s string) (ArticleCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return ArticleCursor{}, fmt.Errorf("%w: %s", ErrInvalidCursor, "not base64url")
	}
	ts, idPart, found := strings.Cut(string(raw), "|")
	if !found {
		return ArticleCursor{}, fmt.Errorf("%w: %s", ErrInvalidCursor, "missing separator")
	}
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil {
		return ArticleCursor{}, fmt.Errorf("%w: %s", ErrInvalidCursor, "bad id")
	}
	c := ArticleCursor{ID: id}
	if ts != "" {
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return ArticleCursor{}, fmt.Errorf("%w: %s", ErrInvalidCursor, "bad timestamp")
		}
		c.PublishedAt = &t
	}
	return c, nil
}
