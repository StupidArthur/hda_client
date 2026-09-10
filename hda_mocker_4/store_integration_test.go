package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
)

func makeTestParquet(t *testing.T, dbpath, out string, values string) {
	t.Helper()
	db, err := openStore(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.db.Exec(`SET autoinstall_known_extensions=false`); err != nil {
		t.Fatal(err)
	}
	q := "COPY (SELECT * FROM (VALUES (TIMESTAMP WITH TIME ZONE '2024-01-01 00:00:00+00', CAST(" + values + " AS DOUBLE)), (TIMESTAMP WITH TIME ZONE '2024-01-01 00:00:01+00', 2.0), (TIMESTAMP WITH TIME ZONE '2024-01-01 00:00:02+00', 3.0)) AS x(Timestamp, tag)) TO ? (FORMAT PARQUET)"
	q += " TO '" + strings.ReplaceAll(out, "'", "''") + "' (FORMAT PARQUET)"
	q = strings.Replace(q, " TO ? (FORMAT PARQUET)", "", 1)
	if _, err := db.db.Exec(q); err != nil {
		t.Fatal(err)
	}
}

func TestDuckDBImportIdempotentConflictAndHistoryPages(t *testing.T) {
	d := t.TempDir()
	dbpath := filepath.Join(d, "history.duckdb")
	p1 := filepath.Join(d, "one.parquet")
	p2 := filepath.Join(d, "two.parquet")
	makeTestParquet(t, dbpath, p1, "1.0")
	s, e := openStore(dbpath)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	f, e := validateParquet(s.db, p1, true)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.importFiles(d, []FileInfo{f})
	if e != nil || r.Samples != 3 {
		t.Fatalf("import result=%+v err=%v", r, e)
	}
	r, e = s.importFiles(d, []FileInfo{f})
	if e != nil || r.Files != 0 {
		t.Fatalf("idempotence result=%+v err=%v", r, e)
	}
	s.Close()
	makeTestParquet(t, dbpath, p2, "9.0")
	s, e = openStore(dbpath)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	f2, e := validateParquet(s.db, p2, true)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.importFiles(d, []FileInfo{f2}); e == nil {
		t.Fatal("expected conflicting sample error")
	}
	if _, e = s.writeLiveAndState(map[string]any{"tag": 4.0}, time.Date(2024, 1, 1, 0, 0, 3, 0, time.UTC), 99, []playbackUpdate{{Path: "da.parquet", SHA: "sha", Next: 7}}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.writeLive(map[string]any{"tag": 5.0}, time.Date(2024, 1, 1, 0, 0, 3, 0, time.UTC), 100); e == nil {
		t.Fatal("expected non-increasing live timestamp error")
	}
	if got, e := s.playbackState("da.parquet", "sha"); e != nil || got != 7 {
		t.Fatalf("playback state got=%d err=%v", got, e)
	}
	s.Close()
	s, e = openStore(dbpath)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = s.Close() }()
	if got := s.lastBatch(); got != 99 {
		t.Fatalf("batch not persisted: %d", got)
	}
	h := newHistory(s, 1, 1)
	defer h.Close()
	dts := &ua.ReadRawModifiedDetails{StartTime: time.Date(2023, 12, 31, 0, 0, 0, 0, time.UTC), EndTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)}
	item := &ua.HistoryReadValueID{NodeID: ua.NewStringNodeID(1, "tag")}
	first := h.page("session", dts, item, false)
	if first.StatusCode != ua.StatusOK || len(first.ContinuationPoint) == 0 {
		t.Fatalf("first page=%v cp=%q", first.StatusCode, first.ContinuationPoint)
	}
	item.ContinuationPoint = first.ContinuationPoint
	second := h.page("session", dts, item, false)
	if second.StatusCode != ua.StatusOK || len(second.ContinuationPoint) == 0 {
		t.Fatalf("second page=%v cp=%q", second.StatusCode, second.ContinuationPoint)
	}
}
