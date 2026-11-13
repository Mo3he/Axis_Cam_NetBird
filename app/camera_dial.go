package main

import (
	"context"
	"net"
	"time"

	"github.com/things-go/go-socks5"
)

// Camera services are dialed on [::1]. The web server's loopback virtual hosts
// differ by AXIS OS version: AXIS OS 13 answers 127.0.0.1 in plain HTTP on every
// port, 443 included, and AXIS OS 12 does the same for other 127.x addresses.
// Both serve [::1] like the network, with TLS on 443.
const (
	cameraHost = "::1"
	// forwardFallback is tried when [::1] fails for a forwarded port. The
	// admin chose those ports, so they may reach services that listen only on
	// 127.0.0.1 (such as port 80 on an HTTPS-only device).
	forwardFallback = "127.0.0.1"
	// socksFallback is tried when [::1] fails for the inbound SOCKS5 proxy,
	// which accepts any port. It reaches services listening on IPv4 0.0.0.0
	// but never those bound to 127.0.0.1 alone, such as other apps' settings
	// back ends, which have no login of their own.
	socksFallback     = "127.0.0.2"
	cameraDialTimeout = 10 * time.Second
)

// dialCamera connects to a camera port on [::1], or on fallback if that fails.
func dialCamera(port, fallback string) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(cameraHost, port), cameraDialTimeout)
	if err != nil {
		return net.DialTimeout("tcp", net.JoinHostPort(fallback, port), cameraDialTimeout)
	}
	return c, err
}

// cameraResolver answers every name with the camera, so the inbound SOCKS5
// proxy never looks names up.
type cameraResolver struct{}

func (cameraResolver) Resolve(ctx context.Context, _ string) (context.Context, net.IP, error) {
	return ctx, net.ParseIP(cameraHost), nil
}

// newInboundSocksServer serves SOCKS5 CONNECT to ports on the camera only,
// whatever destination the peer asks for. Other devices on the camera's
// network are reached through Routing mode, under NetBird's access policies.
func newInboundSocksServer() *socks5.Server {
	return socks5.NewServer(
		socks5.WithRule(&socks5.PermitCommand{EnableConnect: true}),
		socks5.WithResolver(cameraResolver{}),
		socks5.WithDial(func(_ context.Context, _, addr string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			return dialCamera(port, socksFallback)
		}),
	)
}
