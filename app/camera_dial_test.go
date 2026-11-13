package main

import (
	"encoding/binary"
	"io"
	"net"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func listenPort(t *testing.T, network, addr string) (net.Listener, string) {
	t.Helper()
	ln, err := net.Listen(network, addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln, strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func dialedHost(port, fallback string) string {
	c, err := dialCamera(port, fallback)
	if err != nil {
		return "unreachable"
	}
	defer c.Close()
	return c.RemoteAddr().(*net.TCPAddr).IP.String()
}

func requireLinuxLoopback(t *testing.T) {
	t.Helper()
	// The camera runs Linux; 127.0.0.2 needs all of 127.0.0.0/8 on loopback.
	if runtime.GOOS != "linux" {
		t.Skip("needs Linux loopback addressing")
	}
	if ln, err := net.Listen("tcp6", "[::1]:0"); err != nil {
		t.Skip("no IPv6 loopback:", err)
	} else {
		_ = ln.Close()
	}
}

func TestDialCamera(t *testing.T) {
	requireLinuxLoopback(t)
	cases := []struct {
		name           string
		network, addr  string
		forward, socks string
	}{
		// Like the camera's web server: [::1] for both.
		{"all addresses", "tcp", ":0", cameraHost, cameraHost},
		// Reachable from the LAN, so also through SOCKS5.
		{"IPv4 0.0.0.0 only", "tcp4", "0.0.0.0:0", forwardFallback, socksFallback},
		// Internal: forwarded ports may reach it, SOCKS5 never.
		{"127.0.0.1 only", "tcp", "127.0.0.1:0", forwardFallback, "unreachable"},
	}
	for _, c := range cases {
		_, port := listenPort(t, c.network, c.addr)
		if got := dialedHost(port, forwardFallback); got != c.forward {
			t.Errorf("%s, forwarded port: dialed %s, want %s", c.name, got, c.forward)
		}
		if got := dialedHost(port, socksFallback); got != c.socks {
			t.Errorf("%s, SOCKS5: dialed %s, want %s", c.name, got, c.socks)
		}
	}
}

// socksConnect sends a SOCKS5 CONNECT for host:port and returns the reply code.
func socksConnect(t *testing.T, proxy string, atyp byte, host []byte, port int) byte {
	t.Helper()
	c, err := net.DialTimeout("tcp", proxy, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	buf := make([]byte, 2)
	if _, err := c.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	req := []byte{5, 1, 0, atyp}
	if atyp == 3 {
		req = append(req, byte(len(host)))
	}
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 4)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatal(err)
	}
	return reply[1]
}

func TestInboundSocksReachesOnlyTheCamera(t *testing.T) {
	requireLinuxLoopback(t)
	target, port := listenPort(t, "tcp", ":0")
	accepted := make(chan string, 4)
	go func() {
		for {
			c, err := target.Accept()
			if err != nil {
				return
			}
			accepted <- c.LocalAddr().(*net.TCPAddr).IP.String()
			_ = c.Close()
		}
	}()
	proxyLn, _ := listenPort(t, "tcp", "127.0.0.1:0")
	go func() { _ = newInboundSocksServer().Serve(proxyLn) }()
	p, _ := strconv.Atoi(port)

	// Whatever the peer asks for, an address elsewhere or a name, the
	// connection goes to the camera.
	for name, req := range map[string]struct {
		atyp byte
		host []byte
	}{
		"ipv4": {1, []byte{192, 0, 2, 7}},
		"name": {3, []byte("example.invalid")},
	} {
		if code := socksConnect(t, proxyLn.Addr().String(), req.atyp, req.host, p); code != 0 {
			t.Fatalf("%s: CONNECT reply %d, want success", name, code)
		}
		select {
		case got := <-accepted:
			if got != cameraHost {
				t.Errorf("%s: connection arrived on %s, want %s", name, got, cameraHost)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: no connection reached the camera", name)
		}
	}

	// A camera service bound only to 127.0.0.1 is not reachable.
	_, lport := listenPort(t, "tcp", "127.0.0.1:0")
	lp, _ := strconv.Atoi(lport)
	if code := socksConnect(t, proxyLn.Addr().String(), 1, []byte{127, 0, 0, 1}, lp); code == 0 {
		t.Error("loopback-only service reached through the inbound SOCKS5 proxy")
	}
}
