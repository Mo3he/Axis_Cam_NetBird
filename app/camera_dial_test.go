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

func listenPort(t *testing.T, addr string) (net.Listener, string) {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln, strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func TestDialCamera(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs all of 127.0.0.0/8 on the loopback interface, as on Linux")
	}

	// A service on all addresses, like the camera's web server, is reached on
	// 127.0.0.2.
	_, port := listenPort(t, ":0")
	c, err := dialCamera(port, false)
	if err != nil {
		t.Fatalf("wildcard service: %v", err)
	}
	if got := c.RemoteAddr().(*net.TCPAddr).IP.String(); got != cameraHost {
		t.Errorf("wildcard service dialed on %s, want %s", got, cameraHost)
	}
	_ = c.Close()

	// A service bound only to 127.0.0.1 is reached with the fallback and
	// never without it.
	_, port = listenPort(t, "127.0.0.1:0")
	if c, err := dialCamera(port, false); err == nil {
		_ = c.Close()
		t.Error("loopback-only service reached without fallback")
	}
	c, err = dialCamera(port, true)
	if err != nil {
		t.Fatalf("loopback-only service with fallback: %v", err)
	}
	_ = c.Close()
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
	if runtime.GOOS != "linux" {
		t.Skip("needs all of 127.0.0.0/8 on the loopback interface, as on Linux")
	}
	target, port := listenPort(t, ":0")
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

	proxyLn, _ := listenPort(t, "127.0.0.1:0")
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
}
