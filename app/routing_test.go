package main

import (
	"reflect"
	"slices"
	"testing"

	netbird "github.com/netbirdio/netbird/client/embed"
)

func newTestClient(t *testing.T) *netbird.Client {
	t.Helper()
	client, err := netbird.New(netbird.Options{
		DeviceName:    "test",
		PrivateKey:    "aGVsbG8taGVsbG8taGVsbG8taGVsbG8taGVsbG8tMTI=",
		ManagementURL: "https://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("netbird.New: %v", err)
	}
	return client
}

// Guards the reflection override against upstream changes to embed.Client.
func TestEnableServerRoutes(t *testing.T) {
	client := newTestClient(t)
	config, err := client.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !config.DisableServerRoutes {
		t.Fatal("embed.New no longer disables server routes; the override may be obsolete")
	}

	if err := enableServerRoutes(client); err != nil {
		t.Fatalf("enableServerRoutes: %v", err)
	}
	config, err = client.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.DisableServerRoutes {
		t.Fatal("DisableServerRoutes still set after enableServerRoutes")
	}
}

func TestEnableServerRoutesNilClient(t *testing.T) {
	if err := enableServerRoutes(nil); err == nil {
		t.Fatal("enableServerRoutes(nil) succeeded, want error")
	}
}

func TestServedRoutes(t *testing.T) {
	client := newTestClient(t)
	if routes := servedRoutes(client); len(routes) != 0 {
		t.Fatalf("servedRoutes() = %v, want none", routes)
	}

	recorder := unexportedField(client, "recorder")
	add := recorder.MethodByName("AddLocalPeerStateRoute")
	if !add.IsValid() {
		t.Fatal("status recorder has no AddLocalPeerStateRoute")
	}
	add.Call([]reflect.Value{reflect.ValueOf("10.0.0.0/24"), reflect.Zero(add.Type().In(1))})

	routes := servedRoutes(client)
	if _, ok := routes["10.0.0.0/24"]; !ok || len(routes) != 1 {
		t.Fatalf("servedRoutes() = %v, want 10.0.0.0/24", routes)
	}
	if servedRoutes(nil) != nil {
		t.Fatal("servedRoutes(nil) returned routes")
	}
}

func TestParseRoutingMode(t *testing.T) {
	tests := map[string]string{
		"":          routingOff,
		"off":       routingOff,
		" Full ":    routingFull,
		"internet":  routingInternet,
		"exit-node": routingOff,
	}
	for value, want := range tests {
		if got := parseRoutingMode(value); got != want {
			t.Errorf("parseRoutingMode(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestParseBool(t *testing.T) {
	for _, value := range []string{"yes", "YES", " true", "1", "on"} {
		if !parseBool(value) {
			t.Errorf("parseBool(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "no", "false", "0", "maybe"} {
		if parseBool(value) {
			t.Errorf("parseBool(%q) = true, want false", value)
		}
	}
}

func TestRouteSummary(t *testing.T) {
	local := map[string]struct{}{"192.168.1.0/24": {}, "0.0.0.0/0": {}}
	peers := []remotePeer{
		{Name: "gw.netbird.cloud", Routes: map[string]struct{}{"10.1.0.0/16": {}}},
		{Name: "exit.netbird.cloud", Routes: map[string]struct{}{"0.0.0.0/0": {}}},
		{Name: "idle.netbird.cloud"},
	}

	served, used, exitNode := routeSummary(local, peers)
	if want := []string{"0.0.0.0/0", "192.168.1.0/24"}; !slices.Equal(served, want) {
		t.Errorf("served = %v, want %v", served, want)
	}
	wantUsed := []peerRoute{
		{Network: "0.0.0.0/0", Peer: "exit.netbird.cloud"},
		{Network: "10.1.0.0/16", Peer: "gw.netbird.cloud"},
	}
	if !slices.Equal(used, wantUsed) {
		t.Errorf("used = %v, want %v", used, wantUsed)
	}
	if exitNode != "exit.netbird.cloud" {
		t.Errorf("exitNode = %q, want exit.netbird.cloud", exitNode)
	}
}

func TestRouteSummaryEmpty(t *testing.T) {
	served, used, exitNode := routeSummary(nil, nil)
	if served == nil || used == nil || len(served) != 0 || len(used) != 0 || exitNode != "" {
		t.Fatalf("routeSummary(nil, nil) = %v, %v, %q; want empty non-nil slices", served, used, exitNode)
	}
}
