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

	"github.com/gopcua/opcua/ua"
)

type FileInfo struct {
	Path, Name, SHA string
	HDA             bool
	Tags            []string
	StatusCols      map[string]string // optional <tag>.__status columns (OPC UA StatusCode UInt32)
	HDAValueCols    map[string]string // DA-only optional <tag>.__hda_value columns
	HDAStatusCols   map[string]string // DA-only optional <tag>.__hda_status columns
	Rows            int64
	Period          time.Duration
}

const (
	statusSuffix    = ".__status"
	hdaValueSuffix  = ".__hda_value"
	hdaStatusSuffix = ".__hda_status"
)

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
	valueCols := map[string]bool{}
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
		} else if strings.HasSuffix(n, statusSuffix) || strings.HasSuffix(n, hdaStatusSuffix) {
			if !isQualityType(types[i]) {
				return fi, fmt.Errorf("%s: status column %q must be an integer, got %s", path, n, types[i])
			}
		} else if strings.HasSuffix(n, hdaValueSuffix) {
			if hda {
				return fi, fmt.Errorf("%s: HDA must not contain DA-only column %q", path, n)
			}
			if !isNumericType(types[i]) {
				return fi, fmt.Errorf("%s: HDA value column %q must be numeric, got %s", path, n, types[i])
			}
		} else {
			if !isNumericType(types[i]) {
				return fi, fmt.Errorf("%s: column %q must be numeric, got %s", path, n, types[i])
			}
			fi.Tags = append(fi.Tags, n)
			valueCols[n] = true
		}
	}
	fi.StatusCols = map[string]string{}
	fi.HDAValueCols = map[string]string{}
	fi.HDAStatusCols = map[string]string{}
	for _, n := range names {
		var tag string
		switch {
		case strings.HasSuffix(n, statusSuffix):
			tag = strings.TrimSuffix(n, statusSuffix)
			if !valueCols[tag] {
				return fi, fmt.Errorf("%s: status column %q has no matching tag column", path, n)
			}
			fi.StatusCols[tag] = n
		case strings.HasSuffix(n, hdaValueSuffix):
			tag = strings.TrimSuffix(n, hdaValueSuffix)
			if hda || !valueCols[tag] {
				return fi, fmt.Errorf("%s: HDA value column %q has no matching DA tag column", path, n)
			}
			fi.HDAValueCols[tag] = n
		case strings.HasSuffix(n, hdaStatusSuffix):
			tag = strings.TrimSuffix(n, hdaStatusSuffix)
			if hda || !valueCols[tag] {
				return fi, fmt.Errorf("%s: HDA status column %q has no matching DA tag column", path, n)
			}
			fi.HDAStatusCols[tag] = n
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
	// A missing status column defaults to StatusGood (0). A NULL cell is an
	// invalid dataset because it is otherwise indistinguishable from a typo.
	statusCols := append(mapValues(fi.StatusCols), mapValues(fi.HDAStatusCols)...)
	sort.Strings(statusCols)
	if len(statusCols) > 0 {
		exprs := make([]string, 0, len(statusCols)*2)
		values := make([]int64, len(statusCols)*2)
		dest := make([]any, len(values))
		for i, n := range statusCols {
			col := quoteIdent(n)
			exprs = append(exprs,
				fmt.Sprintf("count(*) FILTER (WHERE %s IS NULL)", col),
				fmt.Sprintf("count(*) FILTER (WHERE CAST(%s AS DOUBLE) < 0 OR CAST(%s AS DOUBLE) > 4294967295)", col, col),
			)
			dest[i*2], dest[i*2+1] = &values[i*2], &values[i*2+1]
		}
		query := "SELECT " + strings.Join(exprs, ",") + " FROM read_parquet(?)"
		if err := db.QueryRow(query, path).Scan(dest...); err != nil {
			return fi, err
		}
		for i, n := range statusCols {
			if values[i*2] != 0 {
				return fi, fmt.Errorf("%s: status column %q contains NULL", path, n)
			}
			if values[i*2+1] != 0 {
				return fi, fmt.Errorf("%s: status column %q must be between 0 and 4294967295", path, n)
			}
		}
	}
	fi.Rows = count
	return fi, nil
}
func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
func isNumericType(t string) bool {
	u := strings.ToUpper(t)
	return strings.Contains(u, "DOUBLE")
}

func isQualityType(t string) bool {
	u := strings.ToUpper(t)
	return strings.Contains(u, "INT") && !strings.Contains(u, "INTERVAL")
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
		return nil, uint32(ua.StatusBadWaitingForInitialData)
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
		return nil, uint32(ua.StatusBadWaitingForInitialData)
	}
	return &f, 0
}

// normalizeUAStatus accepts the parquet integer representation of a complete
// OPC UA StatusCode. Dataset validation already rejects NULL, negative and
// out-of-range values; the fallback keeps direct callers safe as well.
func normalizeUAStatus(v any) uint32 {
	if v == nil {
		return 0
	}
	var status uint64
	switch x := v.(type) {
	case int8:
		if x < 0 {
			return uint32(ua.StatusBad)
		}
		status = uint64(x)
	case int16:
		if x < 0 {
			return uint32(ua.StatusBad)
		}
		status = uint64(x)
	case int32:
		if x < 0 {
			return uint32(ua.StatusBad)
		}
		status = uint64(x)
	case int64:
		if x < 0 {
			return uint32(ua.StatusBad)
		}
		status = uint64(x)
	case int:
		if x < 0 {
			return uint32(ua.StatusBad)
		}
		status = uint64(x)
	case uint8:
		status = uint64(x)
	case uint16:
		status = uint64(x)
	case uint32:
		status = uint64(x)
	case uint64:
		status = x
	default:
		return uint32(0x80000000)
	}
	if status > uint64(^uint32(0)) {
		return uint32(0x80000000)
	}
	return uint32(status)
}
