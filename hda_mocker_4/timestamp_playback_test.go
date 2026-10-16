package main

import (
	"encoding/json"
	"fmt"
	"github.com/gopcua/opcua/ua"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFutureHistoryPreflightAndFaultStatus(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "clock.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	future := now.Add(time.Hour)
	_, err = s.db.Exec("INSERT INTO samples VALUES (1,?,1.0,0,'import',1)", future.UnixMicro())
	if err != nil {
		t.Fatal(err)
	}
	p := newPlayback(s, nil)
	if p.validatePlaybackClock(now) == nil {
		t.Fatal("future history must reject startup")
	}
	if p.validatePlaybackClock(future) == nil {
		t.Fatal("equal timestamp must reject startup")
	}
	if err = p.validatePlaybackClock(future.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	v := 1.0
	p.current["a"] = Sample{Name: "a", Value: &v, TS: now}
	f := FileInfo{Path: "test", Tags: []string{"a"}}
	called := false
	p.SetNotify(func(tag string) { x, _ := p.Current(tag); called = x.Quality == uint32(ua.StatusBadOutOfService) })
	p.failTimestampPlayback(f, fmt.Errorf("test write failure"))
	if !called {
		t.Fatal("must notify bad quality after failure")
	}
	if p.filesState[f.Path].LastError == "" {
		t.Fatal("must expose fault")
	}
}

func TestBASFActualImport(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f, err := validateParquet(s.db, "presets/basf_dirty/hda/basf_dirty.parquet", true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.importFiles("presets/basf_dirty", []FileInfo{f})
	if err != nil {
		t.Fatal(err)
	}
	var count, nulls int64
	if err = s.db.QueryRow("SELECT count(*),count(*) FILTER (WHERE value IS NULL) FROM samples").Scan(&count, &nulls); err != nil {
		t.Fatal(err)
	}
	if count != 243248 || nulls != 0 || result.Samples != count {
		t.Fatalf("count=%d nulls=%d reported=%d", count, nulls, result.Samples)
	}
	var first int64
	if err = s.db.QueryRow("SELECT min(ts) FROM samples").Scan(&first); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("presets/basf_dirty/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Start string `json:"start"`
	}
	if err = json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	start, err := time.Parse(time.RFC3339Nano, manifest.Start)
	if err != nil {
		t.Fatal(err)
	}
	want := start.UnixMicro()
	if first != want {
		t.Fatalf("timestamp=%d want=%d", first, want)
	}
}

func TestTimestampPlaybackCircularHold(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "test.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	path := filepath.Join(t.TempDir(), "da.parquet")
	_, err = s.db.Exec("COPY (SELECT * FROM (VALUES (TIMESTAMPTZ '2026-10-13 00:00:00.001+00',NULL::DOUBLE,2.0::DOUBLE),(TIMESTAMPTZ '2026-10-13 00:00:00.021+00',3.0::DOUBLE,NULL::DOUBLE)) t(Timestamp,a,b)) TO '" + filepath.ToSlash(path) + "' (FORMAT PARQUET)")
	if err != nil {
		t.Fatal(err)
	}
	f, err := validateParquet(s.db, path, false)
	if err != nil {
		t.Fatal(err)
	}
	p := newPlayback(s, []FileInfo{f})
	if _, err = s.db.Exec("INSERT INTO tags(tag_id,name) VALUES (1,'a'),(2,'b')"); err != nil {
		t.Fatal(err)
	}
	if err = p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if x, ok := p.Current("a"); !ok || x.Value == nil || *x.Value != 3 {
		t.Fatalf("circular initial value: %+v", x)
	}
	deadline := time.Now().Add(time.Second)
	for {
		p.mu.RLock()
		ts := p.lastCommit
		p.mu.RUnlock()
		if !ts.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no playback")
		}
		time.Sleep(time.Millisecond)
	}
	for tag, want := range map[string]float64{"a": 3, "b": 2} {
		x, ok := p.Current(tag)
		if !ok || x.Value == nil || *x.Value != want {
			t.Fatalf("hold %s: %+v", tag, x)
		}
	}
	deadline = time.Now().Add(2 * time.Second)
	var count int
	for {
		if err = s.db.QueryRow("SELECT count(*) FROM samples").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count >= 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected two playback cycles, got %d", count)
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.Close()
	rows, err := s.db.Query("SELECT t.name,s.value,s.ts FROM samples s JOIN tags t USING(tag_id) ORDER BY ts LIMIT 4")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantNames := []string{"b", "a", "b", "a"}
	wantValues := []float64{2, 3, 2, 3}
	wantOffsets := []int64{0, 20000, 120000, 140000}
	var first int64
	i := 0
	for rows.Next() {
		var name string
		var value float64
		var ts int64
		if err = rows.Scan(&name, &value, &ts); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = ts
		}
		if name != wantNames[i] || value != wantValues[i] || ts-first != wantOffsets[i] {
			t.Fatalf("row %d: %s %v offset=%d", i, name, value, ts-first)
		}
		i++
	}
	if i != 4 {
		t.Fatalf("got %d observations", i)
	}
}
