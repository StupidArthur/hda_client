package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in disk benchmark. Always point HDA_IMPORT_BENCH_DB at a fresh temporary
// database, never at a deployed preset runtime. No server or DA is started.
func TestImportDatasetBenchmark(t *testing.T) {
	root := os.Getenv("HDA_IMPORT_BENCH")
	if root == "" {
		t.Skip("set HDA_IMPORT_BENCH and HDA_IMPORT_BENCH_DB")
	}
	dbPath := os.Getenv("HDA_IMPORT_BENCH_DB")
	if dbPath == "" {
		t.Fatal("fresh benchmark database path required")
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatal("benchmark database must not exist")
	}
	s, err := openStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	paths, err := filepath.Glob(filepath.Join(root, "hda", "*.parquet"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("files=%v err=%v", paths, err)
	}
	files := make([]FileInfo, 0, len(paths))
	for _, path := range paths {
		f, err := validateParquet(s.db, path, true)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	started := time.Now()
	r, err := s.importFilesContext(context.Background(), files, nil)
	if err != nil {
		t.Fatal(err)
	}
	importTime := time.Since(started)
	var count, minTS, maxTS int64
	var digest string
	// Content fingerprint excludes internal batch IDs and file provenance.
	err = s.db.QueryRow(`SELECT count(*), min(ts), max(ts), bit_xor(hash(t.name,s.ts,s.value,s.quality))::VARCHAR FROM samples s JOIN tags t USING(tag_id)`).Scan(&count, &minTS, &maxTS, &digest)
	if err != nil {
		t.Fatal(err)
	}
	if count != r.Samples {
		t.Fatalf("rows=%d reported=%d", count, r.Samples)
	}
	retry, err := s.importFilesContext(context.Background(), files, nil)
	if err != nil || retry.Skipped != len(files) || retry.Samples != 0 {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
	closeStarted := time.Now()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("IMPORT_BENCH files=%d samples=%d import=%s close=%s min_us=%d max_us=%d fingerprint=%s", r.Files, count, importTime, time.Since(closeStarted), minTS, maxTS, digest)
}
