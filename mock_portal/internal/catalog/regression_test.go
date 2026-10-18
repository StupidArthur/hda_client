package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFailedScanRemainsStaleUntilRecovery(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "presets")
	writeFile(t, filepath.Join(root, "one", "config.yaml"), "server:\n  endpoint: opc.tcp://127.0.0.1:4840\n")
	cat, err := New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	good := cat.Snapshot(true)
	if err := os.Rename(root, root+"-saved"); err != nil {
		t.Fatal(err)
	}
	for _, force := range []bool{true, false} {
		snap := cat.Snapshot(force)
		if !snap.Stale || snap.Error == "" || len(snap.Mocks) != 1 || !snap.ScannedAt.Equal(good.ScannedAt) {
			t.Fatalf("lost last good listing or error: %+v", snap)
		}
	}
	if err := os.Rename(root+"-saved", root); err != nil {
		t.Fatal(err)
	}
	if snap := cat.Snapshot(true); snap.Stale || snap.Error != "" {
		t.Fatalf("did not recover: %+v", snap)
	}
}

func TestInitialFailedScanIsNotFreshEmptyCatalog(t *testing.T) {
	cat, _ := New(filepath.Join(t.TempDir(), "missing"), nil)
	for _, force := range []bool{true, false} {
		snap := cat.Snapshot(force)
		if !snap.Stale || snap.Error == "" || !snap.ScannedAt.IsZero() {
			t.Fatalf("failure looks fresh: %+v", snap)
		}
	}
}

func TestLoopbackDiagnosticRequiresLocalUATarget(t *testing.T) {
	for _, host := range []string{"203.0.113.10", "[2001:db8::1]", "remote.example", "0.0.0.0"} {
		for _, listen := range []string{"127.0.0.1:18990", "[::1]:18990", "localhost:18990"} {
			target := deriveDiagTarget(listen, parseEndpoint("opc.tcp://"+host+":4840"))
			if target.State == DiagMonitored || target.URL != "" {
				t.Fatalf("unsafe loopback probe for %s: %+v", host, target)
			}
		}
	}
	for _, host := range []string{"127.0.0.1", "[::1]", "localhost"} {
		if target := deriveDiagTarget("127.0.0.1:18990", parseEndpoint("opc.tcp://"+host+":4840")); target.State != DiagMonitored {
			t.Fatalf("local target rejected: %+v", target)
		}
	}
}
