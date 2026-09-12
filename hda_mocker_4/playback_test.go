package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
)

func TestPlaybackCloseWithoutDA(t *testing.T) {
	p := newPlayback(nil, nil)
	p.Close()
}

func TestDAReadWaitsForFirstPlaybackInsteadOfUsingHistory(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec("INSERT INTO tags VALUES (1, 'tag')"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO samples VALUES (1, ?, 7.0, 0, 'import', 1)", time.Now().UTC().UnixMicro()); err != nil {
		t.Fatal(err)
	}
	p := newPlayback(s, []FileInfo{{Tags: []string{"tag"}}})
	m := &mockServer{store: s, playback: p, daTags: map[string]bool{"tag": true}}
	if got := m.value("tag"); got.Status != ua.StatusBadWaitingForInitialData || got.Has(ua.DataValueValue) {
		t.Fatalf("DA before first playback=%+v", got)
	}
	m.playback = nil
	m.daTags = nil
	if got := m.value("tag"); got.Status != ua.StatusOK || !got.Has(ua.DataValueValue) || got.Value.Value().(float64) != 7 {
		t.Fatalf("pure HDA latest=%+v", got)
	}
}
