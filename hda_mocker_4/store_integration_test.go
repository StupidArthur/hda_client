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
	first := h.page("session", dts, item, false, ua.TimestampsToReturnBoth)
	if first.StatusCode != ua.StatusOK || len(first.ContinuationPoint) == 0 {
		t.Fatalf("first page=%v cp=%q", first.StatusCode, first.ContinuationPoint)
	}
	item.ContinuationPoint = first.ContinuationPoint
	second := h.page("session", dts, item, false, ua.TimestampsToReturnBoth)
	if second.StatusCode != ua.StatusOK || len(second.ContinuationPoint) == 0 {
		t.Fatalf("second page=%v cp=%q", second.StatusCode, second.ContinuationPoint)
	}
}

func TestStatusesAndSeparateDAHDAPlayback(t *testing.T) {
	d := t.TempDir()
	s, err := openStore(filepath.Join(d, "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hda := filepath.Join(d, "hda-status.parquet")
	if _, err = s.db.Exec("COPY (SELECT * FROM (VALUES (TIMESTAMP WITH TIME ZONE '2024-01-01 00:00:00+00', CAST(1.0 AS DOUBLE), CAST(2150694912 AS BIGINT))) AS x(Timestamp, tag, \"tag.__status\")) TO '" + strings.ReplaceAll(hda, "'", "''") + "' (FORMAT PARQUET)"); err != nil {
		t.Fatal(err)
	}
	hf, err := validateParquet(s.db, hda, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.importFiles(d, []FileInfo{hf}); err != nil {
		t.Fatal(err)
	}
	x, err := s.latest("tag")
	if err != nil || x.Quality != uint32(ua.StatusBadNoCommunication) {
		t.Fatalf("HDA status=%#x err=%v", x.Quality, err)
	}

	da := filepath.Join(d, "da-status.parquet")
	q := "COPY (SELECT * FROM (VALUES (CAST(11.0 AS DOUBLE),CAST(2150694912 AS BIGINT),CAST(11.0 AS DOUBLE),CAST(0 AS BIGINT),CAST(999.0 AS DOUBLE),CAST(2156593152 AS BIGINT),CAST(12.0 AS DOUBLE),CAST(0 AS BIGINT),CAST(NULL AS DOUBLE),CAST(2150694912 AS BIGINT),CAST(13.0 AS DOUBLE),CAST(0 AS BIGINT),CAST(14.0 AS DOUBLE),CAST(1083179008 AS BIGINT),CAST(15.0 AS DOUBLE),CAST(2156724224 AS BIGINT),CAST(16.0 AS DOUBLE))) AS x(tag_bad,\"tag_bad.__status\",\"tag_bad.__hda_value\",\"tag_bad.__hda_status\",tag_wrong,\"tag_wrong.__status\",\"tag_wrong.__hda_value\",\"tag_wrong.__hda_status\",tag_empty,\"tag_empty.__status\",\"tag_empty.__hda_value\",\"tag_empty.__hda_status\",tag_diff,\"tag_diff.__status\",\"tag_diff.__hda_value\",\"tag_diff.__hda_status\",tag_default)) TO '" + strings.ReplaceAll(da, "'", "''") + "' (FORMAT PARQUET)"
	if _, err = s.db.Exec(q); err != nil {
		t.Fatal(err)
	}
	df, err := validateParquet(s.db, da, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.registerDA([]FileInfo{df}); err != nil {
		t.Fatal(err)
	}
	p := newPlayback(s, []FileInfo{df})
	p.round([]FileInfo{df})
	checks := []struct {
		name                  string
		daValue               *float64
		daQuality, hdaQuality uint32
		hdaValue              float64
	}{
		{"tag_bad", ptr(11), uint32(ua.StatusBadNoCommunication), uint32(ua.StatusOK), 11},
		{"tag_wrong", ptr(999), uint32(ua.StatusBadDeviceFailure), uint32(ua.StatusOK), 12},
		{"tag_empty", nil, uint32(ua.StatusBadWaitingForInitialData), uint32(ua.StatusOK), 13},
		{"tag_diff", ptr(14), uint32(ua.StatusUncertainLastUsableValue), uint32(ua.StatusBadOutOfService), 15},
		{"tag_default", ptr(16), uint32(ua.StatusOK), uint32(ua.StatusOK), 16},
	}
	for _, c := range checks {
		cur, ok := p.Current(c.name)
		if !ok || cur.Quality != c.daQuality || (cur.Value == nil) != (c.daValue == nil) || cur.Value != nil && *cur.Value != *c.daValue {
			t.Fatalf("DA %s=%+v ok=%v", c.name, cur, ok)
		}
		h, err := s.latest(c.name)
		if err != nil || h.Quality != c.hdaQuality || h.Value == nil || *h.Value != c.hdaValue {
			t.Fatalf("HDA %s=%+v err=%v", c.name, h, err)
		}
	}
}

func ptr(v float64) *float64 { return &v }

func TestStatusColumnRejectsNullAndOutOfRange(t *testing.T) {
	d := t.TempDir()
	s, err := openStore(filepath.Join(d, "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct{ name, value string }{{"null", "CAST(NULL AS BIGINT)"}, {"negative", "CAST(-1 AS BIGINT)"}, {"range", "CAST(4294967296 AS BIGINT)"}} {
		p := filepath.Join(d, tc.name+".parquet")
		q := "COPY (SELECT * FROM (VALUES (CAST(1.0 AS DOUBLE)," + tc.value + ")) AS x(tag,\"tag.__status\")) TO '" + strings.ReplaceAll(p, "'", "''") + "' (FORMAT PARQUET)"
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
		if _, err := validateParquet(s.db, p, false); err == nil {
			t.Fatalf("%s status accepted", tc.name)
		}
	}
}

func TestStatusColumnAcceptsUInt32Max(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := filepath.Join(t.TempDir(), "max-status.parquet")
	q := "COPY (SELECT * FROM (VALUES (CAST(1.0 AS DOUBLE), CAST(4294967295 AS UBIGINT))) AS x(tag, \"tag.__status\")) TO '" + strings.ReplaceAll(p, "'", "''") + "' (FORMAT PARQUET)"
	if _, err := s.db.Exec(q); err != nil {
		t.Fatal(err)
	}
	if _, err := validateParquet(s.db, p, false); err != nil {
		t.Fatalf("UInt32 maximum rejected: %v", err)
	}
}

func TestUAStatusSQLPreservesUInt32(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.db.Query("SELECT status, " + uaStatusSQL("status") + " FROM (VALUES (CAST(0 AS UBIGINT)), (CAST(1083179008 AS UBIGINT)), (CAST(2150694912 AS UBIGINT)), (CAST(4294967295 AS UBIGINT))) t(status)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var q uint64
		var got uint32
		if err := rows.Scan(&q, &got); err != nil {
			t.Fatal(err)
		}
		if want := normalizeUAStatus(q); got != want {
			t.Fatalf("status=%d: SQL=%#x Go=%#x", q, got, want)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
