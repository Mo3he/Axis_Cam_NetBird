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
	"strconv"
	"strings"
	"syscall"
	"time"

	netbird "github.com/netbirdio/netbird/client/embed"
	"github.com/things-go/go-socks5"
)

const (
	defaultManagementURL    = "https://api.netbird.io:443"
	defaultHTTPPort         = "18080"
	defaultSocksPort        = "11080"
	defaultForwardPorts     = "80,443,554"
	defaultInboundSocksPort = "1080"
	maxForwardPorts         = 16
	statusAddress           = "127.0.0.1:2205"
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

	routingMode := parseRoutingMode(os.Getenv("NB_ROUTING_MODE"))
	acceptRoutes := parseBool(os.Getenv("NB_ACCEPT_ROUTES"))

	client, err := netbird.New(netbird.Options{
		DeviceName:    hostname(),
		SetupKey:      setupKey,
		PrivateKey:    privateKey,
		ManagementURL: managementURL,
		ConfigPath:    stateDir + "/config.json",
		StatePath:     stateDir + "/state.json",
		LogOutput:     os.Stderr,
		LogLevel:      "info",
		BlockInbound:  false,
		// Drops routed traffic to the camera's directly attached subnets.
		BlockLANAccess:      routingMode != routingFull,
		DisableClientRoutes: !acceptRoutes,
		DisableIPv6:         true,
	})
	if err != nil {
		log.Fatalf("create NetBird client: %v", err)
	}
	if routingMode != routingOff {
		if err := enableServerRoutes(client); err != nil {
			log.Printf("routing peer unavailable, serving no routes: %v", err)
			routingMode = routingOff
		} else {
			log.Printf("routing peer enabled (mode %s)", routingMode)
		}
	}
	if acceptRoutes {
		log.Printf("accepting routes and exit nodes from other peers")
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
	dialer := newOverlayDialer(client.DialContext, acceptRoutes)
	proxy := &httpProxy{dial: dialer.DialContext}
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
	socksServer := socks5.NewServer(
		socks5.WithDial(dialer.DialContext),
		socks5.WithResolver(passthroughResolver{}),
	)
	go func() {
		log.Printf("SOCKS5 proxy listening on 127.0.0.1:%s", socksPort)
		if err := socksServer.Serve(socksListener); err != nil {
			log.Printf("SOCKS5 proxy: %v", err)
		}
	}()
	forwardPorts := parsePorts(os.Getenv("NB_FORWARD_PORTS"), defaultForwardPorts)
	var overlayListeners []net.Listener
	var forwardedPorts []string
	for _, port := range forwardPorts {
		listener, err := client.ListenTCP(":" + port)
		if err != nil {
			log.Printf("forward overlay port %s: %v", port, err)
			continue
		}
		overlayListeners = append(overlayListeners, listener)
		forwardedPorts = append(forwardedPorts, port)
		go forwardLoop(listener, port)
		log.Printf("forwarding overlay port %s to %s", port, net.JoinHostPort(cameraHost, port))
	}

	inboundSocksPort := os.Getenv("NB_INBOUND_SOCKS5_PORT")
	if inboundSocksPort == "" {
		inboundSocksPort = defaultInboundSocksPort
	}
	inboundSocksAddress := ""
	if inboundListener, err := client.ListenTCP(":" + inboundSocksPort); err != nil {
		log.Printf("inbound SOCKS5 proxy: %v", err)
	} else {
		overlayListeners = append(overlayListeners, inboundListener)
		inboundSocksAddress = "overlay:" + inboundSocksPort
		inboundSocksServer := newInboundSocksServer()
		go func() {
			log.Printf("inbound SOCKS5 proxy listening on overlay port %s", inboundSocksPort)
			if err := inboundSocksServer.Serve(inboundListener); err != nil {
				log.Printf("inbound SOCKS5 proxy: %v", err)
			}
		}()
	}

	runtime := runtimeInfo{
		ProxyAddress:        "127.0.0.1:" + httpPort,
		Socks5Address:       "127.0.0.1:" + socksPort,
		ForwardPorts:        forwardedPorts,
		InboundSocksAddress: inboundSocksAddress,
		RoutingMode:         routingMode,
		AcceptRoutes:        acceptRoutes,
	}
	statusServer := &http.Server{
		Addr: statusAddress,
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writeStatus(writer, client, runtime)
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
	for _, listener := range overlayListeners {
		_ = listener.Close()
	}
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

// parsePorts returns de-duplicated, valid TCP ports from a comma-separated list.
func parsePorts(value, fallback string) []string {
	if strings.TrimSpace(value) == "" {
		value = fallback
	}

	seen := make(map[string]bool)
	var ports []string
	for _, field := range strings.Split(value, ",") {
		port := strings.TrimSpace(field)
		if port == "" || seen[port] {
			continue
		}
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			log.Printf("ignoring invalid forward port %q", port)
			continue
		}
		seen[port] = true
		ports = append(ports, port)
		if len(ports) == maxForwardPorts {
			break
		}
	}
	return ports
}

func forwardLoop(listener net.Listener, port string) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go forwardConn(conn, port)
	}
}

func forwardConn(conn net.Conn, port string) {
	defer conn.Close()

	upstream, err := dialCamera(port, true)
	if err != nil {
		log.Printf("forward to camera port %s: %v", port, err)
		return
	}
	defer upstream.Close()

	done := make(chan struct{}, 2)
	go proxyCopy(done, upstream, conn)
	go proxyCopy(done, conn, upstream)
	<-done
}

type runtimeInfo struct {
	ProxyAddress        string
	Socks5Address       string
	ForwardPorts        []string
	InboundSocksAddress string
	RoutingMode         string
	AcceptRoutes        bool
}

func writeStatus(writer http.ResponseWriter, client *netbird.Client, runtime runtimeInfo) {
	status, err := client.Status()
	response := struct {
		Connected           bool        `json:"connected"`
		ManagementConnected bool        `json:"management_connected"`
		SignalConnected     bool        `json:"signal_connected"`
		OverlayIP           string      `json:"overlay_ip"`
		ManagementURL       string      `json:"management_url"`
		ProxyAddress        string      `json:"proxy_address"`
		Socks5Address       string      `json:"socks5_address"`
		ForwardPorts        []string    `json:"forward_ports"`
		InboundSocks5Port   string      `json:"inbound_socks5_address,omitempty"`
		RoutingMode         string      `json:"routing_mode"`
		AcceptRoutes        bool        `json:"accept_routes"`
		ServedRoutes        []string    `json:"served_routes"`
		PeerRoutes          []peerRoute `json:"peer_routes"`
		ExitNode            string      `json:"exit_node,omitempty"`
		Error               string      `json:"error,omitempty"`
	}{
		ProxyAddress:      runtime.ProxyAddress,
		Socks5Address:     runtime.Socks5Address,
		ForwardPorts:      runtime.ForwardPorts,
		InboundSocks5Port: runtime.InboundSocksAddress,
		RoutingMode:       runtime.RoutingMode,
		AcceptRoutes:      runtime.AcceptRoutes,
		ServedRoutes:      []string{},
		PeerRoutes:        []peerRoute{},
	}
	if err != nil {
		response.Error = err.Error()
	} else {
		response.ManagementConnected = status.ManagementState.Connected
		response.SignalConnected = status.SignalState.Connected
		response.Connected = response.ManagementConnected && response.SignalConnected
		response.OverlayIP = status.LocalPeerState.IP
		response.ManagementURL = status.ManagementState.URL
		peers := make([]remotePeer, 0, len(status.Peers))
		for _, state := range status.Peers {
			if state.Mux == nil {
				continue // offline peers carry no lock and serve no routes
			}
			name := strings.TrimSuffix(state.FQDN, ".")
			if name == "" {
				name = state.IP
			}
			peers = append(peers, remotePeer{Name: name, Routes: state.GetRoutes()})
		}
		response.ServedRoutes, response.PeerRoutes, response.ExitNode = routeSummary(servedRoutes(client), peers)
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
	dial dialFunc
}

func (p *httpProxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodConnect {
		http.Error(writer, "CONNECT required", http.StatusMethodNotAllowed)
		return
	}

	upstream, err := p.dial(request.Context(), "tcp", request.Host)
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
