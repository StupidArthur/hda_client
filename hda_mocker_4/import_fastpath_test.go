package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func importFixture(t *testing.T, s *Store, path, query string) FileInfo {
	t.Helper()
	if _, err := s.db.Exec("COPY (" + query + ") TO '" + strings.ReplaceAll(path, "'", "''") + "' (FORMAT PARQUET)"); err != nil {
		t.Fatal(err)
	}
	f, err := validateParquet(s.db, path, true)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestImportFastPathAndOverlappingFallback(t *testing.T) {
	d := t.TempDir()
	s, err := openStore(filepath.Join(d, "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	query := `SELECT * FROM (VALUES
	 (TIMESTAMPTZ '2026-08-01 00:00:00.197+00', 0.0::DOUBLE, NULL::DOUBLE, 2147483648::BIGINT),
	 (TIMESTAMPTZ '2026-08-01 00:00:00.198+00', NULL::DOUBLE, 2.0::DOUBLE, 0::BIGINT),
	 (TIMESTAMPTZ '2026-08-01 00:00:00.201+00', -3.0::DOUBLE, NULL::DOUBLE, 0::BIGINT)
	 ) t(Timestamp,a,b,"b.__status")`
	f := importFixture(t, s, filepath.Join(d, "first.parquet"), query)
	r, err := s.importFiles(d, []FileInfo{f})
	if err != nil || r.Samples != 4 || r.Files != 1 {
		t.Fatalf("first=%+v err=%v", r, err)
	}
	var badNull, zeros, millis int
	if err = s.db.QueryRow(`SELECT count(*) FILTER(WHERE value IS NULL AND quality=2147483648), count(*) FILTER(WHERE value=0), count(*) FILTER(WHERE ts%1000000=197000) FROM samples`).Scan(&badNull, &zeros, &millis); err != nil {
		t.Fatal(err)
	}
	if badNull != 1 || zeros != 1 || millis != 2 {
		t.Fatalf("bad/null=%d zeros=%d millis=%d", badNull, zeros, millis)
	}
	// A second disjoint tag set takes the same fast path in a nonempty store.
	f2 := importFixture(t, s, filepath.Join(d, "disjoint.parquet"), `SELECT TIMESTAMPTZ '2026-08-01 00:00:00.197+00' AS Timestamp, 7.0::DOUBLE AS c`)
	if r, err = s.importFiles(d, []FileInfo{f2}); err != nil || r.Samples != 1 {
		t.Fatalf("disjoint=%+v err=%v", r, err)
	}
	// Same data, different file path: original idempotent-overlap semantics.
	dup := importFixture(t, s, filepath.Join(d, "duplicate.parquet"), query)
	if r, err = s.importFiles(d, []FileInfo{dup}); err != nil || r.Samples != 4 {
		t.Fatalf("overlap=%+v err=%v", r, err)
	}
	var count int
	s.db.QueryRow("SELECT count(*) FROM samples").Scan(&count)
	if count != 5 {
		t.Fatalf("duplicate rows inserted: %d", count)
	}
	conflict := importFixture(t, s, filepath.Join(d, "conflict.parquet"), `SELECT TIMESTAMPTZ '2026-08-01 00:00:00.197+00' AS Timestamp, 99.0::DOUBLE AS a, 8.0::DOUBLE AS new_tag`)
	if _, err = s.importFiles(d, []FileInfo{conflict}); err == nil || !strings.Contains(err.Error(), "conflicting samples") {
		t.Fatalf("missing conflict: %v", err)
	}
	s.db.QueryRow("SELECT count(*) FROM tags WHERE name='new_tag'").Scan(&count)
	if count != 0 {
		t.Fatal("failed file left tag registration behind")
	}
	s.db.QueryRow("SELECT count(*) FROM imports").Scan(&count)
	if count != 3 {
		t.Fatalf("failed file marked complete: %d", count)
	}
}

func TestImportCancellationReportsOnlyCommittedFiles(t *testing.T) {
	d := t.TempDir()
	s, err := openStore(filepath.Join(d, "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	query := `SELECT TIMESTAMPTZ '2026-08-01 00:00:00.197+00' AS Timestamp, 1.0::DOUBLE AS a`
	f := importFixture(t, s, filepath.Join(d, "one.parquet"), query)
	f2 := importFixture(t, s, filepath.Join(d, "two.parquet"), strings.ReplaceAll(query, " AS a", " AS b"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := s.importFilesContext(ctx, []FileInfo{f, f2}, func(r ImportResult, _ FileInfo) { cancel() })
	if err != context.Canceled || r.Samples != 1 || r.Files != 1 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	var count int
	s.db.QueryRow("SELECT count(*) FROM samples").Scan(&count)
	if count != 1 {
		t.Fatalf("partial file persisted: %d", count)
	}
}

func TestImportInvalidTimelineRollsBackNewTags(t *testing.T) {
	d := t.TempDir()
	s, err := openStore(filepath.Join(d, "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f := importFixture(t, s, filepath.Join(d, "unordered.parquet"), `SELECT * FROM (VALUES
	 (TIMESTAMPTZ '2026-08-01 00:00:00.198+00',1.0::DOUBLE),
	 (TIMESTAMPTZ '2026-08-01 00:00:00.197+00',2.0::DOUBLE)) t(Timestamp,new_tag)`)
	r, err := s.importFiles(d, []FileInfo{f})
	if err == nil || r.Samples != 0 || r.Files != 0 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	for _, table := range []string{"tags", "samples", "imports"} {
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s: count=%d err=%v", table, count, err)
		}
	}
}
