package portalconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndExclusions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portal.yaml")
	if err := os.WriteFile(path, []byte("presets_dir: ./presets\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen != "0.0.0.0:8080" {
		t.Fatalf("listen default = %q", cfg.Listen)
	}
	if len(cfg.ExcludePresets) != 1 || cfg.ExcludePresets[0] != "demo" {
		t.Fatalf("exclusions = %v", cfg.ExcludePresets)
	}
	if cfg.PresetsDir != filepath.Join(dir, "presets") {
		t.Fatalf("presets_dir = %q", cfg.PresetsDir)
	}
}

func TestLoadRespectsAbsoluteDirAndExclusions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "portal.yaml")
	content := "listen: 127.0.0.1:9000\npresets_dir: G:/data/presets\nexclude_presets: [demo, sample]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PresetsDir != filepath.Clean("G:/data/presets") {
		t.Fatalf("presets_dir = %q", cfg.PresetsDir)
	}
	if len(cfg.ExcludePresets) != 2 {
		t.Fatalf("exclusions = %v", cfg.ExcludePresets)
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.yaml")
	if _, err := Load(missing); err == nil {
		t.Fatal("missing config file should fail")
	}
	noDir := filepath.Join(dir, "no-dir.yaml")
	if err := os.WriteFile(noDir, []byte("listen: 127.0.0.1:1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(noDir); err == nil {
		t.Fatal("missing presets_dir should fail")
	}
}
