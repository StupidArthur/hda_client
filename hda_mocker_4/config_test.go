package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigRejectsUnknownField(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "config.yaml")
	if err := os.WriteFile(p, []byte("server:\n  endpoint: opc.tcp://0.0.0.0:4840\n  ns: 3\n  namespace: urn:test\n  max_page_size: 2\n  typo: true\nplayback:\n  files:\n    carousel.parquet:\n      period_ms: 10\nhistory:\n  retention_days: 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConfig(p); err == nil {
		t.Fatal("unknown config field accepted")
	}
}

func TestLoadConfigRejectsMissingNamespaceIndex(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "config.yaml")
	data := "server:\n  endpoint: opc.tcp://0.0.0.0:4840\n  namespace: urn:test\n  max_page_size: 2\nplayback:\n  files: {}\nhistory:\n  retention_days: 0\n"
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConfig(p); err == nil {
		t.Fatal("missing server.ns accepted")
	}
}
