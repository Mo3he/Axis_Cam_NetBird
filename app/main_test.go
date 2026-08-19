package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

func TestPersistedPrivateKey(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(stateDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"PrivateKey":"key-material"}`), 0600); err != nil {
		t.Fatal(err)
	}

	if got := persistedPrivateKey(stateDir); got != "key-material" {
		t.Fatalf("persistedPrivateKey() = %q, want key-material", got)
	}
}

func TestPersistedPrivateKeyMissingConfig(t *testing.T) {
	if got := persistedPrivateKey(t.TempDir()); got != "" {
		t.Fatalf("persistedPrivateKey() = %q, want empty", got)
	}
}

func TestParsePorts(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "empty uses fallback", value: "  ", want: []string{"80", "443", "554"}},
		{name: "trims and dedupes", value: "80, 443 ,80", want: []string{"80", "443"}},
		{name: "drops invalid", value: "22,abc,0,65536,8080", want: []string{"22", "8080"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parsePorts(test.value, "80,443,554")
			if !slices.Equal(got, test.want) {
				t.Fatalf("parsePorts(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestParsePortsLimit(t *testing.T) {
	value := ""
	for port := 1000; port < 1000+maxForwardPorts+5; port++ {
		value += strconv.Itoa(port) + ","
	}

	if got := parsePorts(value, "80"); len(got) != maxForwardPorts {
		t.Fatalf("parsePorts() returned %d ports, want %d", len(got), maxForwardPorts)
	}
}
