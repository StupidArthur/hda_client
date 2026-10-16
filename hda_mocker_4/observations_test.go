package main

import (
	"github.com/gopcua/opcua/ua"
	"path/filepath"
	"testing"
	"time"
)

func TestUnifiedNullBadGood(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "db.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	file := filepath.ToSlash(filepath.Join(t.TempDir(), "observations.parquet"))
	_, err = s.db.Exec("COPY (SELECT * FROM (VALUES (TIMESTAMPTZ '2020-01-01 00:00:00+00',NULL::DOUBLE,0::BIGINT),(TIMESTAMPTZ '2020-01-01 00:00:01+00',NULL::DOUBLE,2150694912::BIGINT),(TIMESTAMPTZ '2020-01-01 00:00:02+00',0.0::DOUBLE,0::BIGINT)) t(Timestamp,tag,\"tag.__status\")) TO '" + file + "' (FORMAT PARQUET)")
	if err != nil {
		t.Fatal(err)
	}
	f, err := validateParquet(s.db, file, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.importFiles("", []FileInfo{f})
	if err != nil || result.Samples != 2 {
		t.Fatalf("import=%+v %v", result, err)
	}
	var q uint32
	var v any
	if err = s.db.QueryRow("SELECT value,quality FROM samples ORDER BY ts LIMIT 1").Scan(&v, &q); err != nil || v != nil || q != uint32(ua.StatusBadNoCommunication) {
		t.Fatalf("bad null: %v %x %v", v, q, err)
	}
	p := newPlayback(s, nil)
	now := time.Now().UTC()
	for i, cell := range []replayValue{{DAValue: 7.0, HDAValue: 7.0}, {}, {DAStatus: uint32(ua.StatusBadNoCommunication), HDAStatus: uint32(ua.StatusBadNoCommunication)}, {}} {
		if err = p.commitObservation(map[string]any{"tag": cell}, now.Add(time.Duration(i)*time.Second), nil); err != nil {
			t.Fatal(err)
		}
		cur, _ := p.Current("tag")
		if i == 1 && (cur.Value == nil || *cur.Value != 7) {
			t.Fatal("missing must hold value")
		}
		if i >= 2 && (cur.Value != nil || cur.Quality != uint32(ua.StatusBadNoCommunication)) {
			t.Fatal("missing must retain bad state")
		}
	}
	var live int
	s.db.QueryRow("SELECT count(*) FROM samples WHERE origin='live'").Scan(&live)
	if live != 2 {
		t.Fatalf("live=%d", live)
	}
}

func TestTimestampQualityOverrides(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "db.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "timed.parquet"))
	_, err = s.db.Exec("COPY (SELECT TIMESTAMPTZ '2020-01-01 00:00:00.123+00' AS Timestamp, NULL::DOUBLE AS tag,2150694912::BIGINT AS \"tag.__status\",12.0::DOUBLE AS \"tag.__hda_value\",0::BIGINT AS \"tag.__hda_status\") TO '" + path + "' (FORMAT PARQUET)")
	if err != nil {
		t.Fatal(err)
	}
	f, err := validateParquet(s.db, path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.registerDA([]FileInfo{f}); err != nil {
		t.Fatal(err)
	}
	p := newPlayback(s, []FileInfo{f})
	if err = p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	deadline := time.Now().Add(time.Second)
	for {
		p.mu.RLock()
		ts := p.lastCommit
		p.mu.RUnlock()
		if !ts.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no commit")
		}
		time.Sleep(time.Millisecond)
	}
	p.Close()
	cur, _ := p.Current("tag")
	if cur.Value != nil || cur.Quality != uint32(ua.StatusBadNoCommunication) {
		t.Fatalf("DA=%+v", cur)
	}
	h, err := s.latest("tag")
	if err != nil || h.Value == nil || *h.Value != 12 || h.Quality != 0 {
		t.Fatalf("HDA=%+v %v", h, err)
	}
}

func TestFixedCircularHoldAndLegacyDatabase(t *testing.T) {
	pathDB := filepath.Join(t.TempDir(), "db.duckdb")
	s, err := openStore(pathDB)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "fixed.parquet"))
	_, err = s.db.Exec("COPY (SELECT * FROM (VALUES (NULL::DOUBLE,0::BIGINT),(5.0::DOUBLE,0::BIGINT),(NULL::DOUBLE,2150694912::BIGINT)) t(tag,\"tag.__status\")) TO '" + path + "' (FORMAT PARQUET)")
	if err != nil {
		t.Fatal(err)
	}
	f, err := validateParquet(s.db, path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.registerDA([]FileInfo{f}); err != nil {
		t.Fatal(err)
	}
	p := newPlayback(s, []FileInfo{f})
	if err = p.initializeFixed(); err != nil {
		t.Fatal(err)
	}
	p.round([]FileInfo{f})
	cur, _ := p.Current("tag")
	if cur.Value != nil || cur.Quality != uint32(ua.StatusBadNoCommunication) {
		t.Fatalf("hold last cycle quality: %+v", cur)
	}
	p.round([]FileInfo{f})
	cur, _ = p.Current("tag")
	if cur.Value == nil || *cur.Value != 5 || cur.Quality != 0 {
		t.Fatalf("recovery: %+v", cur)
	}
	if _, err = s.db.Exec("DELETE FROM state WHERE key='observation_semantics'"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	old, err := openStore(pathDB)
	if err == nil {
		old.Close()
		t.Fatal("must reject nonempty legacy database")
	}
}
