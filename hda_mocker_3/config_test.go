package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStrictYAMLRejectsUnknownFields(t *testing.T) {
	var config Config
	if err := decodeStrictYAML([]byte("server:\n  host: 127.0.0.1\n  typo_port: 1234\nhistory: {}\n"), &config); err == nil {
		t.Fatal("unknown configuration field accepted")
	}
	var preset PresetNodes
	if err := decodeStrictYAML([]byte("dynamic_nodes:\n  count: 1\n  unknown: true\n"), &preset); err == nil {
		t.Fatal("unknown preset field accepted")
	}
}

func TestParseDuration(t *testing.T) {
	got, err := parseDuration("2d")
	if err != nil || got != 48*time.Hour {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := parseDuration("0s"); err == nil {
		t.Fatal("zero duration accepted")
	}
}

func TestLoadSimplePreset(t *testing.T) {
	s, path, err := loadSettings(filepath.Join("presets", "simple", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Port != 48631 || s.DynamicCount != 10 || s.CPMode != "rotating" || path == "" {
		t.Fatalf("bad preset: %+v", s)
	}
}
