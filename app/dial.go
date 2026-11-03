package main

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"time"
)

// connectTimeout bounds each overlay connection attempt. A destination no
// peer routes is dropped silently, so a dial would otherwise wait for the TCP
// SYN retries to run out.
const connectTimeout = 30 * time.Second

type dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

type lookupFunc func(ctx context.Context, network, host string) ([]netip.Addr, error)

// overlayDialer dials through the NetBird overlay for the outbound proxies.
// Names go to NetBird DNS first, which knows peer names and any nameserver
// groups. If it cannot resolve a name, as for public names on an account
// without nameserver groups, and routes from other peers are accepted, the
// camera's system resolver is used and the resulting address is still dialed
// through the overlay, so exit nodes and peer routes apply. Without accepted
// routes no peer could carry such an address, so the DNS error is returned.
type overlayDialer struct {
	dial           dialFunc
	lookup         lookupFunc
	systemFallback bool
	timeout        time.Duration
}

func newOverlayDialer(dial dialFunc, systemFallback bool) *overlayDialer {
	return &overlayDialer{
		dial:           dial,
		lookup:         net.DefaultResolver.LookupNetIP,
		systemFallback: systemFallback,
		timeout:        connectTimeout,
	}
}

// dialOnce applies the connect timeout. The context only governs connection
// setup, so cancelling it afterwards does not affect the connection.
func (d *overlayDialer) dialOnce(ctx context.Context, network, address string) (net.Conn, error) {
	if d.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.timeout)
		defer cancel()
	}
	return d.dial(ctx, network, address)
}

func (d *overlayDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := d.dialOnce(ctx, network, address)
	var dnsErr *net.DNSError
	if err == nil || !d.systemFallback || !errors.As(err, &dnsErr) {
		return conn, err
	}

	host, port, splitErr := net.SplitHostPort(address)
	if splitErr != nil {
		return nil, err
	}
	if _, parseErr := netip.ParseAddr(host); parseErr == nil {
		return nil, err
	}
	// The overlay is IPv4-only (DisableIPv6).
	addresses, lookupErr := d.lookup(ctx, "ip4", host)
	if lookupErr != nil {
		return nil, errors.Join(err, lookupErr)
	}
	if len(addresses) == 0 {
		return nil, err
	}

	var dialErr error
	for _, addr := range addresses {
		conn, dialErr = d.dialOnce(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
		if dialErr == nil {
			return conn, nil
		}
	}
	return nil, dialErr
}

// passthroughResolver keeps SOCKS5 host names unresolved so overlayDialer
// can resolve them, instead of go-socks5 using only the system resolver.
type passthroughResolver struct{}

func (passthroughResolver) Resolve(ctx context.Context, _ string) (context.Context, net.IP, error) {
	return ctx, nil, nil
}
