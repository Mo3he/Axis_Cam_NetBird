package main

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"
)

// fakeOverlay dials only IP literals, failing host names like NetBird DNS
// does on an account without nameserver groups.
type fakeOverlay struct {
	dialed  []string
	refused map[string]bool
}

func (f *fakeOverlay) dial(_ context.Context, _, address string) (net.Conn, error) {
	f.dialed = append(f.dialed, address)
	host, _, _ := net.SplitHostPort(address)
	if _, err := netip.ParseAddr(host); err != nil {
		return nil, &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}}
	}
	if f.refused[address] {
		return nil, errors.New("connection refused")
	}
	client, server := net.Pipe()
	_ = server.Close()
	return client, nil
}

func staticLookup(addrs ...string) lookupFunc {
	return func(_ context.Context, network, _ string) ([]netip.Addr, error) {
		if network != "ip4" {
			return nil, errors.New("want ip4 lookup")
		}
		var out []netip.Addr
		for _, addr := range addrs {
			out = append(out, netip.MustParseAddr(addr))
		}
		return out, nil
	}
}

func TestOverlayDialerFallsBackToSystemDNS(t *testing.T) {
	overlay := &fakeOverlay{refused: map[string]bool{"203.0.113.1:443": true}}
	dialer := &overlayDialer{dial: overlay.dial, lookup: staticLookup("203.0.113.1", "203.0.113.2"), systemFallback: true}

	conn, err := dialer.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	_ = conn.Close()
	want := []string{"example.com:443", "203.0.113.1:443", "203.0.113.2:443"}
	if !slices.Equal(overlay.dialed, want) {
		t.Fatalf("dialed %v, want %v", overlay.dialed, want)
	}
}

func TestOverlayDialerPrefersNetBirdDNS(t *testing.T) {
	looked := false
	dialer := &overlayDialer{
		dial: func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			_ = server.Close()
			return client, nil
		},
		lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			looked = true
			return nil, nil
		},
	}
	conn, err := dialer.DialContext(context.Background(), "tcp", "peer.netbird.cloud:80")
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	_ = conn.Close()
	if looked {
		t.Fatal("system resolver used although NetBird DNS resolved the name")
	}
}

func TestOverlayDialerKeepsNonDNSErrors(t *testing.T) {
	refused := errors.New("connection refused")
	dialer := &overlayDialer{
		dial: func(context.Context, string, string) (net.Conn, error) { return nil, refused },
		lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			t.Fatal("system resolver used for a non-DNS error")
			return nil, nil
		},
		systemFallback: true,
	}
	if _, err := dialer.DialContext(context.Background(), "tcp", "example.com:443"); !errors.Is(err, refused) {
		t.Fatalf("DialContext error = %v, want %v", err, refused)
	}
}

func TestOverlayDialerSystemLookupFailure(t *testing.T) {
	overlay := &fakeOverlay{}
	lookupErr := errors.New("system lookup failed")
	dialer := &overlayDialer{
		dial:           overlay.dial,
		lookup:         func(context.Context, string, string) ([]netip.Addr, error) { return nil, lookupErr },
		systemFallback: true,
	}
	_, err := dialer.DialContext(context.Background(), "tcp", "missing.example:443")
	var dnsErr *net.DNSError
	if !errors.Is(err, lookupErr) || !errors.As(err, &dnsErr) {
		t.Fatalf("DialContext error = %v, want both NetBird and system lookup errors", err)
	}
}

func TestOverlayDialerNoFallbackWithoutAcceptedRoutes(t *testing.T) {
	overlay := &fakeOverlay{}
	dialer := &overlayDialer{
		dial: overlay.dial,
		lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			t.Fatal("system resolver used although routes are not accepted")
			return nil, nil
		},
	}
	_, err := dialer.DialContext(context.Background(), "tcp", "example.com:443")
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) {
		t.Fatalf("DialContext error = %v, want the NetBird DNS error", err)
	}
}

func TestOverlayDialerConnectTimeout(t *testing.T) {
	dialer := &overlayDialer{
		dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
		timeout: 20 * time.Millisecond,
	}
	start := time.Now()
	_, err := dialer.DialContext(context.Background(), "tcp", "192.0.2.1:443")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DialContext error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("DialContext took %v, want the connect timeout", elapsed)
	}
}

func TestPassthroughResolver(t *testing.T) {
	ctx := context.Background()
	gotCtx, ip, err := passthroughResolver{}.Resolve(ctx, "example.com")
	if gotCtx != ctx || ip != nil || err != nil {
		t.Fatalf("Resolve() = %v, %v, %v; want unchanged context and no IP", gotCtx, ip, err)
	}
}
