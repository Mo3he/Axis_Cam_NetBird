package main

import (
	"os"
	"path/filepath"
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
