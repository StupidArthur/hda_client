package main

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSparseTimestampPlaybackOverridesAndEmptyAnchors(t *testing.T) {
	d := t.TempDir()
	s, err := openStore(filepath.Join(d, "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	path := filepath.Join(d, "da.parquet")
	query := `SELECT * FROM (VALUES
	 (TIMESTAMPTZ '2026-09-01 00:00:00.000+00',NULL::DOUBLE,0::BIGINT,NULL::DOUBLE,0::BIGINT,NULL::DOUBLE,NULL::DOUBLE,0::BIGINT),
	 (TIMESTAMPTZ '2026-09-01 00:00:00.020+00',NULL::DOUBLE,0::BIGINT,9.0::DOUBLE,0::BIGINT,0.0::DOUBLE,NULL::DOUBLE,0::BIGINT),
	 (TIMESTAMPTZ '2026-09-01 00:00:00.040+00',NULL::DOUBLE,2147483648::BIGINT,NULL::DOUBLE,0::BIGINT,-2.0::DOUBLE,NULL::DOUBLE,0::BIGINT),
	 (TIMESTAMPTZ '2026-09-01 00:00:00.060+00',NULL::DOUBLE,0::BIGINT,NULL::DOUBLE,0::BIGINT,NULL::DOUBLE,NULL::DOUBLE,2147483648::BIGINT),
	 (TIMESTAMPTZ '2026-09-01 00:00:00.100+00',NULL::DOUBLE,0::BIGINT,NULL::DOUBLE,0::BIGINT,NULL::DOUBLE,NULL::DOUBLE,0::BIGINT)
	 ) t(Timestamp,a,"a.__status","a.__hda_value","a.__hda_status",b,"b.__hda_value","b.__hda_status")`
	if _, err = s.db.Exec("COPY (" + query + ") TO '" + strings.ReplaceAll(path, "'", "''") + "' (FORMAT PARQUET)"); err != nil {
		t.Fatal(err)
	}
	f, err := validateParquet(s.db, path, false)
	if err != nil {
		t.Fatal(err)
	}
	f.Period = 100 * time.Millisecond
	if err = s.registerDA([]FileInfo{f}); err != nil {
		t.Fatal(err)
	}
	p := newPlayback(s, []FileInfo{f})
	var sawZero, sawNegative atomic.Bool
	p.SetNotify(func(tag string) {
		if tag == "b" {
			value, _ := p.Current(tag)
			if value.Value != nil && *value.Value == 0 {
				sawZero.Store(true)
			}
			if value.Value != nil && *value.Value == -2 {
				sawNegative.Store(true)
			}
		}
	})
	if err = p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var count int
		if err = s.db.QueryRow("SELECT count(*) FROM samples").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count >= 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected two rounds of HDA-only observations; got %d", count)
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.Close()
	if !sawZero.Load() || !sawNegative.Load() {
		t.Fatal("DA-only zero/negative observations were skipped")
	}
	rows, err := s.db.Query("SELECT t.name,s.value,s.quality,s.ts FROM samples s JOIN tags t USING(tag_id) ORDER BY ts LIMIT 4")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantNames := []string{"a", "b", "a", "b"}
	wantOffsets := []int64{0, 40000, 200000, 240000}
	i := 0
	var first int64
	for rows.Next() {
		var name string
		var value *float64
		var quality uint32
		var ts int64
		if err = rows.Scan(&name, &value, &quality, &ts); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = ts
		}
		if i >= 4 || name != wantNames[i] || ts-first != wantOffsets[i] {
			t.Fatalf("row %d name=%s offset=%d", i, name, ts-first)
		}
		if name == "a" && (value == nil || *value != 9 || quality != 0) {
			t.Fatalf("lost HDA-only value: %v %d", value, quality)
		}
		if name == "b" && (value != nil || quality != 2147483648) {
			t.Fatalf("lost HDA-only Bad: %v %d", value, quality)
		}
		i++
	}
	if i != 4 {
		t.Fatalf("rows=%d", i)
	}
	a, _ := p.Current("a")
	b, _ := p.Current("b")
	if a.Value != nil || a.Quality != 2147483648 || b.Value == nil || *b.Value != -2 || b.Quality != 0 {
		t.Fatalf("DA-only/hold changed: a=%+v b=%+v", a, b)
	}
}
