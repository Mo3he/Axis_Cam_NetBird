package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	netbird "github.com/netbirdio/netbird/client/embed"
	"github.com/things-go/go-socks5"
)

const (
	defaultManagementURL = "https://api.netbird.io:443"
	defaultHTTPPort      = "18080"
	defaultSocksPort     = "11080"
	statusAddress        = "127.0.0.1:2205"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	setupKey := strings.TrimSpace(os.Getenv("NB_SETUP_KEY"))

	managementURL := os.Getenv("NB_MANAGEMENT_URL")
	if managementURL == "" {
		managementURL = defaultManagementURL
	}

	stateDir := os.Getenv("NB_STATE_DIR")
	if stateDir == "" {
		stateDir = "/usr/local/packages/NetBird_VPN/localdata"
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		log.Fatalf("create state directory: %v", err)
	}
	privateKey := ""
	if setupKey == "" {
		privateKey = persistedPrivateKey(stateDir)
	}
	if setupKey == "" && privateKey == "" {
		log.Fatal("NB_SETUP_KEY is required for first enrollment")
	}

	client, err := netbird.New(netbird.Options{
		DeviceName:          hostname(),
		SetupKey:            setupKey,
		PrivateKey:          privateKey,
		ManagementURL:       managementURL,
		ConfigPath:          stateDir + "/config.json",
		StatePath:           stateDir + "/state.json",
		LogOutput:           os.Stderr,
		LogLevel:            "info",
		BlockInbound:        false,
		BlockLANAccess:      true,
		DisableClientRoutes: true,
		DisableIPv6:         true,
	})
	if err != nil {
		log.Fatalf("create NetBird client: %v", err)
	}

	startCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	err = client.Start(startCtx)
	cancel()
	if err != nil {
		log.Fatalf("start NetBird client: %v", err)
	}
	log.Printf("NetBird client connected")
	if err := os.WriteFile(stateDir+"/setup_key_clear", []byte("ok\n"), 0600); err != nil {
		log.Printf("write setup-key sentinel: %v", err)
	}

	httpPort := os.Getenv("NB_HTTP_PROXY_PORT")
	if httpPort == "" {
		httpPort = defaultHTTPPort
	}
	proxy := &httpProxy{client: client}
	server := &http.Server{Addr: "127.0.0.1:" + httpPort, Handler: proxy}
	go func() {
		log.Printf("HTTP CONNECT proxy listening on 127.0.0.1:%s", httpPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP proxy: %v", err)
		}
	}()
	socksPort := os.Getenv("NB_SOCKS5_PORT")
	if socksPort == "" {
		socksPort = defaultSocksPort
	}
	socksListener, err := net.Listen("tcp", "127.0.0.1:"+socksPort)
	if err != nil {
		log.Fatalf("SOCKS5 proxy: %v", err)
	}
	socksServer := socks5.NewServer(socks5.WithDial(client.DialContext))
	go func() {
		log.Printf("SOCKS5 proxy listening on 127.0.0.1:%s", socksPort)
		if err := socksServer.Serve(socksListener); err != nil {
			log.Printf("SOCKS5 proxy: %v", err)
		}
	}()
	statusServer := &http.Server{
		Addr: statusAddress,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writeStatus(writer, client, httpPort, socksPort)
		}),
	}
	go func() {
		if err := statusServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("status API: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	_ = socksListener.Close()
	_ = statusServer.Shutdown(shutdownCtx)
	if err := client.Stop(shutdownCtx); err != nil {
		log.Printf("NetBird shutdown: %v", err)
	}
}

func persistedPrivateKey(stateDir string) string {
	data, err := os.ReadFile(stateDir + "/config.json")
	if err != nil {
		return ""
	}
	var config struct {
		PrivateKey string
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return ""
	}
	return strings.TrimSpace(config.PrivateKey)
}

func writeStatus(writer http.ResponseWriter, client *netbird.Client, httpPort, socks5Port string) {
	status, err := client.Status()
	response := struct {
		Connected           bool   `json:"connected"`
		ManagementConnected bool   `json:"management_connected"`
		SignalConnected     bool   `json:"signal_connected"`
		OverlayIP           string `json:"overlay_ip"`
		ManagementURL       string `json:"management_url"`
		ProxyAddress        string `json:"proxy_address"`
		Socks5Address       string `json:"socks5_address"`
		Error               string `json:"error,omitempty"`
	}{ProxyAddress: "127.0.0.1:" + httpPort, Socks5Address: "127.0.0.1:" + socks5Port}
	if err != nil {
		response.Error = err.Error()
	} else {
		response.ManagementConnected = status.ManagementState.Connected
		response.SignalConnected = status.SignalState.Connected
		response.Connected = response.ManagementConnected && response.SignalConnected
		response.OverlayIP = status.LocalPeerState.IP
		response.ManagementURL = status.ManagementState.URL
		if status.ManagementState.Error != nil {
			response.Error = status.ManagementState.Error.Error()
		} else if status.SignalState.Error != nil {
			response.Error = status.SignalState.Error.Error()
		}
	}

	writer.Header().Set("Content-Type", "application/json")
	if !response.Connected {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(writer).Encode(response)
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "axis-camera"
	}
	return name
}

type httpProxy struct {
	client *netbird.Client
}

func (p *httpProxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodConnect {
		http.Error(writer, "CONNECT required", http.StatusMethodNotAllowed)
		return
	}

	upstream, err := p.client.DialContext(request.Context(), "tcp", request.Host)
	if err != nil {
		http.Error(writer, fmt.Sprintf("NetBird dial failed: %v", err), http.StatusBadGateway)
		return
	}
	defer upstream.Close()

	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		http.Error(writer, "hijacking is not supported", http.StatusInternalServerError)
		return
	}
	clientConn, buffered, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer clientConn.Close()
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	if buffered.Reader.Buffered() > 0 {
		_, _ = io.Copy(upstream, buffered)
	}

	done := make(chan struct{}, 2)
	go proxyCopy(done, upstream, clientConn)
	go proxyCopy(done, clientConn, upstream)
	<-done
}

func proxyCopy(done chan<- struct{}, destination net.Conn, source net.Conn) {
	_, _ = io.Copy(destination, source)
	_ = destination.SetDeadline(time.Now())
	done <- struct{}{}
}
