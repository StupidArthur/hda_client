package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

type Sample struct {
	TagID   int64
	Name    string
	TS      time.Time
	Value   *float64
	Quality uint32
	Origin  string
	BatchID int64
}
type Tag struct {
	ID   int64
	Name string
}
type ImportResult struct {
	Files   int // newly imported files
	Skipped int // unchanged files already committed in this database
	Samples int64
	Elapsed time.Duration
}

type Store struct {
	db        *sql.DB
	writeMu   sync.Mutex
	nextBatch int64
}

func openStore(path string) (*Store, error) {
	db, e := sql.Open("duckdb", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(8)
	s := &Store{db: db}
	if e = s.init(); e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) init() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS state (key VARCHAR PRIMARY KEY, value VARCHAR)`,
		`CREATE TABLE IF NOT EXISTS tags (tag_id BIGINT PRIMARY KEY, name VARCHAR UNIQUE NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS imports (path VARCHAR PRIMARY KEY, sha256 VARCHAR NOT NULL, rows BIGINT NOT NULL, completed BOOLEAN NOT NULL, imported_at BIGINT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS samples (tag_id BIGINT NOT NULL, ts BIGINT NOT NULL, value DOUBLE, quality BIGINT NOT NULL, origin VARCHAR NOT NULL, batch_id BIGINT NOT NULL, UNIQUE(tag_id,ts))`,
		`CREATE TABLE IF NOT EXISTS playback_state (path VARCHAR PRIMARY KEY, sha256 VARCHAR NOT NULL, next_row BIGINT NOT NULL)`,
	}
	for _, q := range stmts {
		if _, e := s.db.Exec(q); e != nil {
			return e
		}
	}
	if _, e := s.db.Exec(`INSERT INTO state(key,value) VALUES ('last_batch','0') ON CONFLICT(key) DO NOTHING`); e != nil {
		return e
	}
	if _, e := s.db.Exec(`INSERT INTO state(key,value) VALUES ('last_live_time','') ON CONFLICT(key) DO NOTHING`); e != nil {
		return e
	}
	return nil
}
func (s *Store) tagMap() (map[string]Tag, error) {
	rows, e := s.db.Query("SELECT tag_id,name FROM tags ORDER BY tag_id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]Tag{}
	for rows.Next() {
		var t Tag
		if e := rows.Scan(&t.ID, &t.Name); e != nil {
			return nil, e
		}
		out[t.Name] = t
	}
	return out, rows.Err()
}
func (s *Store) tags() ([]Tag, error) {
	m, e := s.tagMap()
	if e != nil {
		return nil, e
	}
	out := make([]Tag, 0, len(m))
	for _, t := range m {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// importFiles keeps the command-line import contract. GUI startup uses the
// context-aware variant so a stop request can roll back the active file safely.
func (s *Store) importFiles(root string, files []FileInfo) (ImportResult, error) {
	return s.importFilesContext(context.Background(), files, nil)
}

// importProgress is emitted only after a whole parquet file has committed.
// A file is one DuckDB transaction, so the reported progress never claims
// rows that could later be rolled back.
type importProgress func(ImportResult, FileInfo)

func (s *Store) importFilesContext(ctx context.Context, files []FileInfo, progress importProgress) (ImportResult, error) {
	started := time.Now()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	total := ImportResult{}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		sha, e := sha256File(f.Path)
		if e != nil {
			return total, e
		}
		var oldSHA string
		var complete bool
		e = s.db.QueryRowContext(ctx, "SELECT sha256,completed FROM imports WHERE path=?", f.Path).Scan(&oldSHA, &complete)
		if e == nil {
			if oldSHA != sha {
				return total, fmt.Errorf("input changed after import: %s", f.Path)
			}
			if complete {
				total.Skipped++
				total.Elapsed = time.Since(started)
				if progress != nil {
					progress(total, f)
				}
				continue
			}
		} else if e != sql.ErrNoRows {
			return total, e
		}
		tx, e := s.db.BeginTx(ctx, nil)
		if e != nil {
			return total, e
		}
		// Registration is atomic with sample rows and import completion.
		m, e := s.tagMapTx(tx)
		if e != nil {
			tx.Rollback()
			return total, e
		}
		maxID := int64(0)
		for _, t := range m {
			if t.ID > maxID {
				maxID = t.ID
			}
		}
		for _, name := range f.Tags {
			if _, ok := m[name]; !ok {
				maxID++
				if _, e = tx.Exec("INSERT INTO tags(tag_id,name) VALUES (?,?)", maxID, name); e != nil {
					tx.Rollback()
					return total, e
				}
				m[name] = Tag{ID: maxID, Name: name}
			}
		}
		qcols := make([]string, len(f.Tags))
		for i, n := range f.Tags {
			qcols[i] = quoteIdent(n)
		}
		selectCols := strings.Join(qcols, ",")
		if f.HDA {
			selectCols = quoteIdent("Timestamp") + "," + selectCols
		}
		batch, e := nextBatchTx(tx)
		if e != nil {
			tx.Rollback()
			return total, e
		}
		var nullTS int64
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM read_parquet(?) WHERE Timestamp IS NULL`, f.Path).Scan(&nullTS); e != nil {
			tx.Rollback()
			return total, e
		}
		if nullTS != 0 {
			tx.Rollback()
			return total, fmt.Errorf("import %s: Timestamp contains NULL", f.Path)
		}
		var badOrder int64
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT Timestamp, lag(Timestamp) OVER (ORDER BY rn) AS prev FROM (SELECT Timestamp, row_number() OVER () AS rn FROM read_parquet(?)) src_order) q WHERE prev IS NOT NULL AND epoch_us(Timestamp) <= epoch_us(prev)`, f.Path).Scan(&badOrder); e != nil {
			tx.Rollback()
			return total, e
		}
		if badOrder != 0 {
			tx.Rollback()
			return total, fmt.Errorf("import %s: Timestamp must be strictly increasing after microsecond normalization", f.Path)
		}
		parts := make([]string, 0, len(f.Tags))
		args := []any{f.Path}
		for _, name := range f.Tags {
			col := quoteIdent(name)
			quality := uaStatusSQL("0")
			if statusCol, ok := f.StatusCols[name]; ok {
				quality = uaStatusSQL(quoteIdent(statusCol))
			}
			parts = append(parts, fmt.Sprintf("SELECT ?::BIGINT AS tag_id, epoch_us(Timestamp)::BIGINT AS ts, CASE WHEN %s IS NULL OR isnan(%s) OR isinf(%s) THEN NULL ELSE %s END AS value, CASE WHEN %s IS NULL OR isnan(%s) OR isinf(%s) THEN 2150760448 ELSE %s END AS quality, 'import' AS origin, ?::BIGINT AS batch_id FROM src", col, col, col, col, col, col, col, quality))
			args = append(args, m[name].ID, batch)
		}
		q := "WITH src AS (SELECT * FROM read_parquet(?)), incoming AS (" + strings.Join(parts, " UNION ALL ") + ") SELECT count(*) FROM incoming i JOIN samples s ON s.tag_id=i.tag_id AND s.ts=i.ts WHERE NOT ((i.value IS NULL AND s.value IS NULL) OR i.value=s.value) OR i.quality<>s.quality"
		var conflicts int64
		if e = tx.QueryRowContext(ctx, q, args...).Scan(&conflicts); e != nil {
			tx.Rollback()
			return total, e
		}
		if conflicts > 0 {
			tx.Rollback()
			return total, fmt.Errorf("import %s: conflicting samples", f.Path)
		}
		insertQ := "WITH src AS (SELECT * FROM read_parquet(?)), incoming AS (" + strings.Join(parts, " UNION ALL ") + ") INSERT INTO samples SELECT i.* FROM incoming i WHERE NOT EXISTS (SELECT 1 FROM samples s WHERE s.tag_id=i.tag_id AND s.ts=i.ts)"
		if _, e = tx.ExecContext(ctx, insertQ, args...); e != nil {
			tx.Rollback()
			return total, e
		}
		total.Samples += f.Rows * int64(len(f.Tags))
		if _, e = tx.Exec("INSERT INTO imports(path,sha256,rows,completed,imported_at) VALUES (?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET sha256=excluded.sha256,rows=excluded.rows,completed=excluded.completed,imported_at=excluded.imported_at", f.Path, sha, f.Rows, true, time.Now().UTC().UnixMicro()); e != nil {
			tx.Rollback()
			return total, e
		}
		if e = tx.Commit(); e != nil {
			return total, e
		}
		total.Files++
		total.Elapsed = time.Since(started)
		if progress != nil {
			progress(total, f)
		}
		log.Printf("import complete file=%s samples=%d elapsed=%s", filepath.Base(f.Path), f.Rows*int64(len(f.Tags)), time.Since(started))
	}
	total.Elapsed = time.Since(started)
	return total, nil
}
func (s *Store) tagMapTx(tx *sql.Tx) (map[string]Tag, error) {
	rows, e := tx.Query("SELECT tag_id,name FROM tags")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	m := map[string]Tag{}
	for rows.Next() {
		var t Tag
		if e := rows.Scan(&t.ID, &t.Name); e != nil {
			return nil, e
		}
		m[t.Name] = t
	}
	return m, rows.Err()
}
func nextBatchTx(tx *sql.Tx) (int64, error) {
	var v string
	if e := tx.QueryRow(`SELECT value FROM state WHERE key='last_batch'`).Scan(&v); e != nil {
		return 0, e
	}
	var n int64
	if _, e := fmt.Sscan(v, &n); e != nil {
		return 0, e
	}
	n++
	if _, e := tx.Exec(`UPDATE state SET value=? WHERE key='last_batch'`, fmt.Sprint(n)); e != nil {
		return 0, e
	}
	return n, nil
}
func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func (s *Store) samples(name string, start, end time.Time, forward bool, after *time.Time, limit int) ([]Sample, error) {
	return s.samplesBound(name, start, end, forward, after, limit, 0)
}
func (s *Store) samplesBound(name string, start, end time.Time, forward bool, after *time.Time, limit int, maxBatch int64) ([]Sample, error) {
	m, e := s.tagMap()
	if e != nil {
		return nil, e
	}
	t, ok := m[name]
	if !ok {
		return nil, fmt.Errorf("unknown tag %q", name)
	}
	q := `SELECT tag_id,ts,value,quality,origin,batch_id FROM samples WHERE tag_id=? AND ts>=? AND ts<=?`
	args := []any{t.ID, start.UTC().UnixMicro(), end.UTC().UnixMicro()}
	if maxBatch > 0 {
		q += ` AND batch_id<=?`
		args = append(args, maxBatch)
	}
	if after != nil {
		if forward {
			q += ` AND ts>?`
		} else {
			q += ` AND ts<?`
		}
		args = append(args, after.UTC().UnixMicro())
	}
	if forward {
		q += ` ORDER BY ts ASC`
	} else {
		q += ` ORDER BY ts DESC`
	}
	q += ` LIMIT ?`
	args = append(args, limit)
	rows, e := s.db.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Sample{}
	for rows.Next() {
		var x Sample
		var micros int64
		if e := rows.Scan(&x.TagID, &micros, &x.Value, &x.Quality, &x.Origin, &x.BatchID); e != nil {
			return nil, e
		}
		x.TS = time.UnixMicro(micros).UTC()
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) latest(name string) (*Sample, error) {
	m, e := s.tagMap()
	if e != nil {
		return nil, e
	}
	t, ok := m[name]
	if !ok {
		return nil, fmt.Errorf("unknown tag")
	}
	var x Sample
	var micros int64
	e = s.db.QueryRow(`SELECT tag_id,ts,value,quality,origin,batch_id FROM samples WHERE tag_id=? ORDER BY ts DESC LIMIT 1`, t.ID).Scan(&x.TagID, &micros, &x.Value, &x.Quality, &x.Origin, &x.BatchID)
	if e == sql.ErrNoRows {
		return nil, nil
	}
	x.TS = time.UnixMicro(micros).UTC()
	return &x, e
}
func (s *Store) hasSamplesAfter(name string, start, end time.Time, forward bool, after *time.Time, maxBatch int64) (bool, error) {
	x, e := s.samplesBound(name, start, end, forward, after, 1, maxBatch)
	return len(x) > 0, e
}
func (s *Store) lastBatch() int64 {
	var x sql.NullInt64
	if e := s.db.QueryRow(`SELECT max(batch_id) FROM samples`).Scan(&x); e != nil {
		log.Printf("read last batch failed: %v", e)
	}
	if x.Valid {
		return x.Int64
	}
	return 0
}
func (s *Store) cleanup(retention int) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if retention <= 0 {
		return nil
	}
	cut := time.Now().UTC().Add(-time.Duration(retention) * 24 * time.Hour)
	_, e := s.db.Exec(`DELETE FROM samples WHERE origin='live' AND ts<?`, cut.UnixMicro())
	return e
}

func (s *Store) registerDA(files []FileInfo) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer func() {
		if e != nil {
			tx.Rollback()
		}
	}()
	m, e := s.tagMapTx(tx)
	if e != nil {
		return e
	}
	max := int64(0)
	for _, t := range m {
		if t.ID > max {
			max = t.ID
		}
	}
	seen := map[string]bool{}
	for _, f := range files {
		for _, n := range f.Tags {
			if seen[n] {
				return fmt.Errorf("duplicate tag in da directory: %s", n)
			}
			seen[n] = true
			if _, ok := m[n]; !ok {
				max++
				if _, e = tx.Exec("INSERT INTO tags(tag_id,name) VALUES (?,?)", max, n); e != nil {
					return e
				}
				m[n] = Tag{ID: max, Name: n}
			}
		}
	}
	return tx.Commit()
}

func (s *Store) playbackRow(f FileInfo, row int64) (map[string]any, error) {
	names, _, e := scanParquetSchema(s.db, f.Path)
	if e != nil {
		return nil, e
	}
	cols := make([]string, len(names))
	for i, n := range names {
		cols[i] = quoteIdent(n)
	}
	q := "SELECT " + strings.Join(cols, ",") + " FROM read_parquet(?) LIMIT 1 OFFSET ?"
	r, e := s.db.Query(q, f.Path, row)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	if !r.Next() {
		return nil, sql.ErrNoRows
	}
	vals := make([]any, len(names))
	ptr := make([]any, len(vals))
	for i := range vals {
		ptr[i] = &vals[i]
	}
	if e = r.Scan(ptr...); e != nil {
		return nil, e
	}
	out := map[string]any{}
	for i, n := range names {
		out[n] = vals[i]
	}
	return out, nil
}
func (s *Store) playbackState(path string, sha string) (int64, error) {
	var old string
	var row int64
	e := s.db.QueryRow(`SELECT sha256,next_row FROM playback_state WHERE path=?`, path).Scan(&old, &row)
	if e == sql.ErrNoRows {
		return 0, nil
	}
	if e != nil {
		return 0, e
	}
	if old != sha {
		return 0, nil
	}
	return row, nil
}
func (s *Store) savePlaybackState(path, sha string, row int64) error {
	_, e := s.db.Exec(`INSERT INTO playback_state(path,sha256,next_row) VALUES (?,?,?) ON CONFLICT(path) DO UPDATE SET sha256=excluded.sha256,next_row=excluded.next_row`, path, sha, row)
	return e
}

type playbackUpdate struct {
	Path, SHA string
	Next      int64
}

type replayValue struct {
	DAValue, DAStatus   any
	HDAValue, HDAStatus any
}

func (s *Store) writeLive(vals map[string]any, ts time.Time, batch int64) ([]Sample, error) {
	return s.writeLiveAndState(vals, ts, batch, nil)
}
func (s *Store) writeLiveAndState(vals map[string]any, ts time.Time, batch int64, updates []playbackUpdate) ([]Sample, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, e := s.db.Begin()
	if e != nil {
		return nil, e
	}
	m, e := s.tagMapTx(tx)
	if e != nil {
		tx.Rollback()
		return nil, e
	}
	var last string
	if e = tx.QueryRow(`SELECT value FROM state WHERE key='last_live_time'`).Scan(&last); e != nil {
		tx.Rollback()
		return nil, e
	}
	if last != "" {
		var micros int64
		if _, e = fmt.Sscan(last, &micros); e != nil {
			tx.Rollback()
			return nil, e
		}
		if ts.UTC().UnixMicro() <= micros {
			tx.Rollback()
			return nil, fmt.Errorf("live timestamp %d is not after last timestamp %d", ts.UTC().UnixMicro(), micros)
		}
	}
	ids := make([]string, 0, len(vals))
	args := make([]any, 0, len(vals))
	for n := range vals {
		ids = append(ids, "?")
		args = append(args, m[n].ID)
	}
	if len(ids) > 0 {
		var latest sql.NullInt64
		q := "SELECT max(ts) FROM samples WHERE tag_id IN (" + strings.Join(ids, ",") + ")"
		if e = tx.QueryRow(q, args...).Scan(&latest); e != nil {
			tx.Rollback()
			return nil, e
		}
		if latest.Valid && ts.UTC().UnixMicro() <= latest.Int64 {
			tx.Rollback()
			return nil, fmt.Errorf("live timestamp %d is not after existing timestamp %d for DA tags", ts.UTC().UnixMicro(), latest.Int64)
		}
	}
	out := make([]Sample, 0, len(vals))
	for n, v := range vals {
		t, ok := m[n]
		if !ok {
			tx.Rollback()
			return nil, fmt.Errorf("unknown DA tag %q", n)
		}
		fv, q := normalizeValue(v)
		if cell, ok := v.(replayValue); ok {
			fv, q = normalizeValue(cell.HDAValue)
			if fv != nil {
				q = normalizeUAStatus(cell.HDAStatus)
			}
		}
		if _, e = tx.Exec(`INSERT INTO samples(tag_id,ts,value,quality,origin,batch_id) VALUES (?,?,?,?,?,?)`, t.ID, ts.UTC().UnixMicro(), fv, q, "live", batch); e != nil {
			tx.Rollback()
			return nil, e
		}
		out = append(out, Sample{TagID: t.ID, Name: n, TS: ts.UTC(), Value: fv, Quality: q, Origin: "live", BatchID: batch})
	}
	for _, u := range updates {
		if _, e = tx.Exec(`INSERT INTO playback_state(path,sha256,next_row) VALUES (?,?,?) ON CONFLICT(path) DO UPDATE SET sha256=excluded.sha256,next_row=excluded.next_row`, u.Path, u.SHA, u.Next); e != nil {
			tx.Rollback()
			return nil, e
		}
	}
	if _, e = tx.Exec(`UPDATE state SET value=? WHERE key='last_live_time'`, fmt.Sprint(ts.UTC().UnixMicro())); e != nil {
		tx.Rollback()
		return nil, e
	}
	if _, e = tx.Exec(`UPDATE state SET value=? WHERE key='last_batch'`, fmt.Sprint(batch)); e != nil {
		tx.Rollback()
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return out, nil
}

// uaStatusSQL preserves the complete OPC UA StatusCode. Schema validation
// guarantees an integer value in the UInt32 range before this expression runs.
func uaStatusSQL(status string) string {
	return fmt.Sprintf("CAST(%s AS BIGINT)", status)
}
