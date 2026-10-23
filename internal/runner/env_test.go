package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCandidateDirsIncludesHerdNodeBins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, "Library", "Application Support", "Herd", "config", "nvm", "versions", "node", "v22.22.3", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}

	found := false
	for _, dir := range candidateDirs() {
		if dir == bin {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("candidateDirs() did not include Herd node bin %q", bin)
	}
}

func TestLookupAgentBinaryFindsHerdNpx(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	bin := filepath.Join(home, "Library", "Application Support", "Herd", "config", "nvm", "versions", "node", "v22.22.3", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	npx := filepath.Join(bin, "npx")
	if err := os.WriteFile(npx, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := LookupAgentBinary("npx")
	if err != nil {
		t.Fatalf("LookupAgentBinary(npx): %v", err)
	}
	if got != npx {
		t.Fatalf("LookupAgentBinary(npx) = %q, want %q", got, npx)
	}
}

func TestCompareVersions(t *testing.T) {
	if compareVersions("v22.22.3", "v9.11.2") <= 0 {
		t.Fatal("v22.22.3 must sort after v9.11.2")
	}
	if compareVersions("v22.2.0", "v22.10.0") >= 0 {
		t.Fatal("v22.2.0 must sort before v22.10.0")
	}
}
