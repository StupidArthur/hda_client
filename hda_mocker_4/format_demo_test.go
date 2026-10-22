package main

import (
	"path/filepath"
	"testing"
	"time"
)

// Actual schema/import validation only: no UA listener, DA loop or preset runtime.
func TestFormatDemoOfflineImport(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	root := filepath.Join("presets", "format_demo")
	hda, err := validateParquet(s.db, filepath.Join(root, "hda", "format_demo.parquet"), true)
	if err != nil {
		t.Fatal(err)
	}
	da, err := validateParquet(s.db, filepath.Join(root, "da", "format_demo.parquet"), false)
	if err != nil {
		t.Fatal(err)
	}
	if hda.Rows != 8 || da.Rows != 8 || len(hda.Tags) != 3 || len(da.Tags) != 3 || !da.Timestamped {
		t.Fatal("unexpected demo schema")
	}
	r, err := s.importFiles(root, []FileInfo{hda})
	if err != nil || r.Samples != 15 || r.Files != 1 {
		t.Fatalf("import=%+v err=%v", r, err)
	}
	var count, bad, nulls, zeros, negatives, goodLocal int
	var first, last int64
	err = s.db.QueryRow(`SELECT count(*),count(*) FILTER(WHERE (quality & 2147483648)<>0),count(*) FILTER(WHERE value IS NULL),count(*) FILTER(WHERE value=0),count(*) FILTER(WHERE value<0),count(*) FILTER(WHERE quality=9830400),min(ts),max(ts) FROM samples`).Scan(&count, &bad, &nulls, &zeros, &negatives, &goodLocal, &first, &last)
	if err != nil {
		t.Fatal(err)
	}
	if count != 15 || bad != 2 || nulls != 1 || zeros != 1 || negatives != 1 || goodLocal != 1 {
		t.Fatalf("counts=%d bad=%d nulls=%d zeros=%d negatives=%d goodLocal=%d", count, bad, nulls, zeros, negatives, goodLocal)
	}
	wantFirst := time.Date(2026, 9, 1, 0, 0, 0, 125000000, time.UTC).UnixMicro()
	if first != wantFirst || last != wantFirst+10000000 {
		t.Fatalf("time precision/window mismatch: %d %d", first, last)
	}
	for name, want := range map[string]int{"sample_temperature": 6, "sample_pressure": 4, "sample_quality": 5} {
		var got int
		if err := s.db.QueryRow("SELECT count(*) FROM samples s JOIN tags t USING(tag_id) WHERE t.name=?", name).Scan(&got); err != nil || got != want {
			t.Fatalf("%s=%d want=%d err=%v", name, got, want, err)
		}
	}
	retry, err := s.importFiles(root, []FileInfo{hda})
	if err != nil || retry.Skipped != 1 || retry.Samples != 0 {
		t.Fatalf("repeat import=%+v err=%v", retry, err)
	}
}
