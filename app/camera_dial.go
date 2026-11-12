package main

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"

	"github.com/things-go/go-socks5"
)

// Camera services are dialed on 127.0.0.2, not 127.0.0.1. Since AXIS OS 13 the
// web server answers 127.0.0.1 in plain HTTP on every port, 443 included, so
// HTTPS over the overlay failed. Any other loopback address reaches the same
// listeners LAN clients use.
const (
	cameraHost        = "127.0.0.2"
	loopbackHost      = "127.0.0.1"
	cameraDialTimeout = 10 * time.Second
)

// dialCamera connects to a camera port. With fallback, a port refused on
// 127.0.0.2 is retried on 127.0.0.1, so a forwarded service bound only to
// 127.0.0.1 stays reachable. The inbound SOCKS5 proxy accepts any port, so it
// never falls back: loopback-only services must not become reachable from the
// overlay.
func dialCamera(port string, fallback bool) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(cameraHost, port), cameraDialTimeout)
	if err != nil && fallback && errors.Is(err, syscall.ECONNREFUSED) {
		return net.DialTimeout("tcp", net.JoinHostPort(loopbackHost, port), cameraDialTimeout)
	}
	return c, err
}

// cameraResolver answers every name with the camera, so the inbound SOCKS5
// proxy never looks names up.
type cameraResolver struct{}

func (cameraResolver) Resolve(ctx context.Context, _ string) (context.Context, net.IP, error) {
	return ctx, net.ParseIP(cameraHost), nil
}

// newInboundSocksServer serves SOCKS5 CONNECT to camera ports only, whatever
// destination the peer asks for.
func newInboundSocksServer() *socks5.Server {
	return socks5.NewServer(
		socks5.WithRule(&socks5.PermitCommand{EnableConnect: true}),
		socks5.WithResolver(cameraResolver{}),
		socks5.WithDial(func(_ context.Context, _, addr string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			return dialCamera(port, false)
		}),
	)
}
