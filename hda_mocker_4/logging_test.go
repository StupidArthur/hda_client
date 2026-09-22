package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRollingLogWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	w, err := newRollingLogWriter(path, 20, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"first-line\n", "second-line\n", "third-line\n"} {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "third-line") || !strings.Contains(string(previous), "second-line") {
		t.Fatalf("unexpected rotation current=%q previous=%q", current, previous)
	}
}

func TestRollingLogWriterRejectsInvalidSettings(t *testing.T) {
	if _, err := newRollingLogWriter(filepath.Join(t.TempDir(), "app.log"), 0, 0); err == nil {
		t.Fatal("invalid settings accepted")
	}
}
