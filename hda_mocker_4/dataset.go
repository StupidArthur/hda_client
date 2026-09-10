package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type FileInfo struct {
	Path, Name, SHA string
	HDA             bool
	Tags            []string
	Rows            int64
	Period          time.Duration
}

func scanParquetSchema(db *sql.DB, path string) ([]string, []string, error) {
	rows, err := db.Query("DESCRIBE SELECT * FROM read_parquet(?)", path)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var names, types []string
	for rows.Next() {
		var n, t string
		var rest [4]any
		if err := rows.Scan(&n, &t, &rest[0], &rest[1], &rest[2], &rest[3]); err != nil {
			return nil, nil, err
		}
		names = append(names, n)
		types = append(types, t)
	}
	return names, types, rows.Err()
}

func validateParquet(db *sql.DB, path string, hda bool) (FileInfo, error) {
	fi := FileInfo{Path: path, Name: filepath.Base(path), HDA: hda}
	names, types, err := scanParquetSchema(db, path)
	if err != nil {
		return fi, fmt.Errorf("%s: schema: %w", path, err)
	}
	if len(names) == 0 {
		return fi, fmt.Errorf("%s: empty schema", path)
	}
	seen := map[string]bool{}
	for i, n := range names {
		if strings.TrimSpace(n) == "" {
			return fi, fmt.Errorf("%s: blank column name", path)
		}
		if seen[n] {
			return fi, fmt.Errorf("%s: duplicate column %q", path, n)
		}
		seen[n] = true
		if n == "Timestamp" {
			if !hda {
				return fi, fmt.Errorf("%s: DA must not contain Timestamp", path)
			}
			if !strings.Contains(strings.ToUpper(types[i]), "TIMESTAMP") || !strings.Contains(strings.ToUpper(types[i]), "TIME ZONE") {
				return fi, fmt.Errorf("%s: Timestamp must be timestamp type", path)
			}
		} else if hda || n != "Timestamp" {
			if !isNumericType(types[i]) {
				return fi, fmt.Errorf("%s: column %q must be numeric, got %s", path, n, types[i])
			}
			fi.Tags = append(fi.Tags, n)
		}
	}
	if hda {
		if names[0] != "Timestamp" {
			return fi, fmt.Errorf("%s: first HDA column must be Timestamp", path)
		}
	} else if len(fi.Tags) == 0 {
		return fi, fmt.Errorf("%s: DA needs at least one tag", path)
	}
	var count int64
	if err := db.QueryRow("SELECT count(*) FROM read_parquet(?)", path).Scan(&count); err != nil {
		return fi, err
	}
	if count < 1 {
		return fi, fmt.Errorf("%s: must contain at least one row", path)
	}
	fi.Rows = count
	return fi, nil
}
func isNumericType(t string) bool {
	u := strings.ToUpper(t)
	return strings.Contains(u, "DOUBLE")
}
func sha256File(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	return fmt.Sprintf("%x", h.Sum(nil)), e
}
func listParquet(dir string) ([]string, error) {
	es, e := os.ReadDir(dir)
	if e != nil {
		return nil, e
	}
	var out []string
	for _, x := range es {
		if !x.IsDir() && strings.EqualFold(filepath.Ext(x.Name()), ".parquet") {
			out = append(out, filepath.Join(dir, x.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

func normalizeTime(v any) (time.Time, error) {
	switch x := v.(type) {
	case time.Time:
		return x.UTC().Truncate(time.Microsecond), nil
	case string:
		t, e := time.Parse(time.RFC3339Nano, x)
		return t.UTC().Truncate(time.Microsecond), e
	default:
		return time.Time{}, fmt.Errorf("Timestamp returned %T", v)
	}
}
func normalizeValue(v any) (*float64, uint32) {
	if v == nil {
		return nil, uint32(0x809B0000)
	}
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case float32:
		f = float64(x)
	case int64:
		f = float64(x)
	case int32:
		f = float64(x)
	case int:
		f = float64(x)
	case uint64:
		f = float64(x)
	case uint32:
		f = float64(x)
	default:
		return nil, 0
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, uint32(0x809B0000)
	}
	return &f, 0
}
