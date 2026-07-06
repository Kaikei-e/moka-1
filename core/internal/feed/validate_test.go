package feed

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeResolver は ipResolver のテストフェイク(DNS を引かない)。
type fakeResolver struct {
	ips map[string][]string
}

func (f *fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	ips, ok := f.ips[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	addrs := make([]net.IPAddr, 0, len(ips))
	for _, s := range ips {
		addrs = append(addrs, net.IPAddr{IP: net.ParseIP(s)})
	}
	return addrs, nil
}

func TestURLValidatorValidate(t *testing.T) {
	t.Parallel()

	resolver := &fakeResolver{ips: map[string][]string{
		"public.example.com":  {"93.184.216.34"},
		"private.example.com": {"10.1.2.3"},
		"mixed.example.com":   {"93.184.216.34", "192.168.0.7"},
		"tailnet.example.com": {"100.100.1.2"},
	}}

	tests := []struct {
		name         string
		url          string
		allowPrivate bool
		wantErr      error // nil = 通る
	}{
		{name: "public https url is valid", url: "https://public.example.com/feed.xml"},
		{name: "public http url is valid", url: "http://public.example.com/feed.xml"},
		{name: "public ip literal is valid", url: "http://93.184.216.34/feed.xml"},

		{name: "ftp scheme is rejected", url: "ftp://example.com/feed", wantErr: ErrInvalidURL},
		{name: "scheme-less url is rejected", url: "example.com/feed.xml", wantErr: ErrInvalidURL},
		{name: "garbage is rejected", url: "://not a url", wantErr: ErrInvalidURL},
		{name: "missing host is rejected", url: "http:///feed.xml", wantErr: ErrInvalidURL},
		{name: "unresolvable host is rejected", url: "http://nxdomain.example.com/feed", wantErr: ErrInvalidURL},

		{name: "loopback literal is blocked", url: "http://127.0.0.1/feed", wantErr: ErrPrivateHost},
		{name: "ipv6 loopback literal is blocked", url: "http://[::1]/feed", wantErr: ErrPrivateHost},
		{name: "rfc1918 10/8 literal is blocked", url: "http://10.0.0.5/feed", wantErr: ErrPrivateHost},
		{name: "rfc1918 192.168/16 literal is blocked", url: "http://192.168.1.1:8080/feed", wantErr: ErrPrivateHost},
		{name: "link-local literal is blocked", url: "http://169.254.1.1/feed", wantErr: ErrPrivateHost},
		{name: "unspecified literal is blocked", url: "http://0.0.0.0/feed", wantErr: ErrPrivateHost},

		{name: "cgnat 100.64/10 literal is blocked", url: "http://100.100.1.2/feed", wantErr: ErrPrivateHost},
		{name: "ietf protocol 192.0.0/24 literal is blocked", url: "http://192.0.0.5/feed", wantErr: ErrPrivateHost},
		{name: "benchmark 198.18/15 literal is blocked", url: "http://198.19.0.9/feed", wantErr: ErrPrivateHost},
		{name: "multicast literal is blocked", url: "http://224.0.1.1/feed", wantErr: ErrPrivateHost},
		{name: "broadcast literal is blocked", url: "http://255.255.255.255/feed", wantErr: ErrPrivateHost},
		{name: "ipv4-mapped ipv6 cgnat literal is blocked", url: "http://[::ffff:100.64.0.1]/feed", wantErr: ErrPrivateHost},

		{name: "hostname resolving to private ip is blocked", url: "http://private.example.com/feed", wantErr: ErrPrivateHost},
		{name: "hostname resolving to cgnat ip is blocked", url: "http://tailnet.example.com/feed", wantErr: ErrPrivateHost},
		{name: "hostname with any private ip is blocked", url: "http://mixed.example.com/feed", wantErr: ErrPrivateHost},

		{name: "allow-private lets loopback through", url: "http://127.0.0.1/feed", allowPrivate: true},
		{name: "allow-private lets private hostname through", url: "http://private.example.com/feed", allowPrivate: true},
		{name: "allow-private still rejects bad scheme", url: "ftp://example.com/feed", allowPrivate: true, wantErr: ErrInvalidURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v := &URLValidator{AllowPrivate: tt.allowPrivate, Resolver: resolver}
			err := v.Validate(t.Context(), tt.url)

			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestNewURLValidatorUsesDefaultResolver(t *testing.T) {
	t.Parallel()

	v := NewURLValidator(true)
	require.NotNil(t, v.Resolver)
	// AllowPrivate=true なら解決なしで通る(ループバックでも可)
	assert.NoError(t, v.Validate(t.Context(), "http://127.0.0.1/feed"))
}
