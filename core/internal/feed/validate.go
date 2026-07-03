package feed

import (
	"context"
	"fmt"
	"net"
	"net/url"
)

// ipResolver は SSRF 検査のための最小 DNS 境界(*net.Resolver が満たす)。
type ipResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// URLValidator はフィード URL のスキーム検査 + SSRF ガード。
// 既知の残余リスク: validate→fetch 間の TOCTOU(DNS rebinding)。リダイレクト毎の
// 再検証(HTTPFetcher の CheckRedirect)で緩和し、dial 時 Control 検査は将来課題。
type URLValidator struct {
	// AllowPrivate はプライベート/ループバック IP を許可する(e2e・ローカル開発用)。
	// スキーム検査は緩和しない。
	AllowPrivate bool
	Resolver     ipResolver
}

// NewURLValidator は net.DefaultResolver を使う検証器を返す。
func NewURLValidator(allowPrivate bool) *URLValidator {
	return &URLValidator{AllowPrivate: allowPrivate, Resolver: net.DefaultResolver}
}

// Validate は raw がフィード URL として妥当か検査する。
// 失敗は ErrInvalidURL(形式・スキーム・解決不能)か ErrPrivateHost(SSRF ブロック)。
func (v *URLValidator) Validate(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse %q: %w", raw, ErrInvalidURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q: %w", u.Scheme, ErrInvalidURL)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host in %q: %w", raw, ErrInvalidURL)
	}

	// IP リテラルは DNS を引かずに判定する
	if ip := net.ParseIP(host); ip != nil {
		if !v.AllowPrivate && isPrivateIP(ip) {
			return fmt.Errorf("ip %s: %w", ip, ErrPrivateHost)
		}
		return nil
	}

	if v.AllowPrivate {
		return nil // 解決結果を検査しないなら DNS を引く必要もない(失敗は fetch 側で顕在化)
	}

	addrs, err := v.Resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, ErrInvalidURL)
	}
	for _, a := range addrs {
		if isPrivateIP(a.IP) {
			return fmt.Errorf("host %s resolves to %s: %w", host, a.IP, ErrPrivateHost)
		}
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}
