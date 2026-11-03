package main

import (
	"errors"
	"log"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"unsafe"

	netbird "github.com/netbirdio/netbird/client/embed"
)

// Routing modes for serving NetBird routes (Network Routes, Networks
// resources and exit nodes) assigned to this camera in the dashboard.
const (
	routingOff      = "off"
	routingInternet = "internet"
	routingFull     = "full"
)

// parseRoutingMode normalises the RoutingMode parameter, falling back to off.
func parseRoutingMode(value string) string {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case routingOff, routingInternet, routingFull:
		return mode
	case "":
		return routingOff
	}
	log.Printf("ignoring invalid routing mode %q, using %s", value, routingOff)
	return routingOff
}

// parseBool accepts the yes/no style used by ACAP parameters.
func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "1", "on":
		return true
	}
	return false
}

// enableServerRoutes clears DisableServerRoutes on a client created by
// embed.New, which hard-codes it to true and has no option to change it.
// Must be called before Start so login, the firewall and the route manager
// all see the routing peer configuration.
func enableServerRoutes(client *netbird.Client) error {
	config := unexportedField(client, "config")
	if !config.IsValid() || config.Kind() != reflect.Pointer || config.IsNil() {
		return errors.New("embedded client has no config")
	}
	flag := config.Elem().FieldByName("DisableServerRoutes")
	if !flag.IsValid() || flag.Kind() != reflect.Bool || !flag.CanSet() {
		return errors.New("embedded client config has no DisableServerRoutes field")
	}
	flag.SetBool(false)
	return nil
}

// unexportedField returns a usable copy of an unexported field of the
// embedded client, or an invalid Value if it does not exist.
func unexportedField(client *netbird.Client, name string) reflect.Value {
	if client == nil {
		return reflect.Value{}
	}
	field := reflect.ValueOf(client).Elem().FieldByName(name)
	if !field.IsValid() {
		return reflect.Value{}
	}
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
}

// servedRoutes returns the routes this camera currently serves. Client.Status
// exposes the recorder's live route map, which the route manager mutates
// concurrently, so read a locked clone through GetLocalPeerState instead.
func servedRoutes(client *netbird.Client) map[string]struct{} {
	recorder := unexportedField(client, "recorder")
	if !recorder.IsValid() || recorder.Kind() != reflect.Pointer || recorder.IsNil() {
		return nil
	}
	method := recorder.MethodByName("GetLocalPeerState")
	if !method.IsValid() || method.Type().NumIn() != 0 || method.Type().NumOut() != 1 {
		return nil
	}
	state := method.Call(nil)[0]
	if state.Kind() != reflect.Struct {
		return nil
	}
	field := state.FieldByName("Routes")
	if !field.IsValid() || !field.CanInterface() {
		return nil
	}
	routes, ok := field.Interface().(map[string]struct{})
	if !ok {
		return nil
	}
	return routes
}

type peerRoute struct {
	Network string `json:"network"`
	Peer    string `json:"peer"`
}

// remotePeer is the subset of a peer's status needed to report routes.
type remotePeer struct {
	Name   string
	Routes map[string]struct{}
}

// routeSummary returns the routes served by this camera, the routes it uses
// through other peers, and the exit node in use (if any).
func routeSummary(localRoutes map[string]struct{}, peers []remotePeer) (served []string, used []peerRoute, exitNode string) {
	served = []string{}
	used = []peerRoute{}
	for network := range localRoutes {
		served = append(served, network)
	}
	slices.Sort(served)

	for _, remote := range peers {
		for network := range remote.Routes {
			used = append(used, peerRoute{Network: network, Peer: remote.Name})
		}
	}
	slices.SortFunc(used, func(a, b peerRoute) int {
		if c := strings.Compare(a.Network, b.Network); c != 0 {
			return c
		}
		return strings.Compare(a.Peer, b.Peer)
	})
	for _, route := range used {
		if isDefaultRoute(route.Network) {
			exitNode = route.Peer
			break
		}
	}
	return served, used, exitNode
}

func isDefaultRoute(network string) bool {
	prefix, err := netip.ParsePrefix(network)
	return err == nil && prefix.Bits() == 0
}
