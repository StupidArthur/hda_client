package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Opt-in same-input DA cache comparison without loading HDA or starting a server.
// Default 10k rows limits risk on the old dense implementation.
func TestDATimestampMemoryBenchmark(t *testing.T) {
	path := os.Getenv("HDA_DA_MEMORY_SOURCE")
	if path == "" {
		t.Skip("set HDA_DA_MEMORY_SOURCE")
	}
	limit := 10000
	if text := os.Getenv("HDA_DA_MEMORY_ROWS"); text != "" {
		var err error
		limit, err = strconv.Atoi(text)
		if err != nil || limit <= 0 {
			t.Fatal("invalid row limit")
		}
	}
	dir := t.TempDir()
	s, err := openStore(filepath.Join(dir, "history.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fixture := filepath.Join(dir, "da.parquet")
	query := "COPY (SELECT * FROM read_parquet('" + strings.ReplaceAll(path, "'", "''") + "') LIMIT " + strconv.Itoa(limit) + ") TO '" + strings.ReplaceAll(fixture, "'", "''") + "' (FORMAT PARQUET)"
	if _, err = s.db.Exec(query); err != nil {
		t.Fatal(err)
	}
	f, err := validateParquet(s.db, fixture, false)
	if err != nil {
		t.Fatal(err)
	}
	f.Period = 100 * time.Millisecond
	if err = s.registerDA([]FileInfo{f}); err != nil {
		t.Fatal(err)
	}
	p := newPlayback(s, []FileInfo{f})
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	if err = p.Start(); err != nil {
		t.Fatal(err)
	}
	loadTime := time.Since(started)
	defer p.Close()
	runtime.GC()
	runtime.ReadMemStats(&after)
	for _, tag := range f.Tags {
		if _, ok := p.Current(tag); !ok {
			t.Fatalf("no current state for %s", tag)
		}
	}
	t.Logf("DA_MEMORY_BENCH rows=%d tags=%d load=%s retained_heap_delta_bytes=%d total_alloc_delta_bytes=%d", f.Rows, len(f.Tags), loadTime, int64(after.HeapAlloc)-int64(before.HeapAlloc), after.TotalAlloc-before.TotalAlloc)
}
