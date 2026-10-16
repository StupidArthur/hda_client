package main

import (
	"database/sql"
	"fmt"
	"github.com/gopcua/opcua/ua"
	"time"
)

func (p *Playback) initializeFixed() error {
	for _, f := range p.files {
		sha, err := sha256File(f.Path)
		if err != nil {
			return err
		}
		cursor, err := p.store.playbackState(f.Path, sha)
		if err != nil {
			return err
		}
		if cursor >= f.Rows {
			cursor = 0
		}
		for _, tag := range f.Tags {
			col := quoteIdent(tag)
			quality := "0::BIGINT"
			if status, ok := f.StatusCols[tag]; ok {
				quality = quoteIdent(status)
			}
			query := fmt.Sprintf("SELECT %s,%s FROM (SELECT *,row_number() OVER ()-1 AS replay_row FROM read_parquet(?)) WHERE isfinite(%s) OR (%s & 2147483648)<>0 ORDER BY (replay_row < ?) DESC,replay_row DESC LIMIT 1", col, quality, col, quality)
			var value, status any
			err = p.store.db.QueryRow(query, f.Path, cursor).Scan(&value, &status)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			v, q, present := observation(value, status)
			if !present {
				q = uint32(ua.StatusBadWaitingForInitialData)
			}
			p.current[tag] = Sample{Name: tag, Value: v, Quality: q, TS: time.Now().UTC(), Origin: "initial"}
		}
	}
	return nil
}

// Missing means neither a finite value nor an explicit Bad quality.
func observation(value, status any) (*float64, uint32, bool) {
	v, _ := normalizeValue(value)
	q := normalizeUAStatus(status)
	return v, q, v != nil || q&0x80000000 != 0
}

func replayCell(f FileInfo, row map[string]any, tag string) replayValue {
	cell := replayValue{DAValue: row[tag], DAStatus: int64(0), HDAValue: row[tag], HDAStatus: int64(0)}
	if col, ok := f.StatusCols[tag]; ok {
		cell.DAStatus = row[col]
	}
	// Without an HDA override, quality follows the actual DA observation.
	cell.HDAStatus = cell.DAStatus
	if col, ok := f.HDAValueCols[tag]; ok {
		cell.HDAValue = row[col]
	}
	if col, ok := f.HDAStatusCols[tag]; ok {
		cell.HDAStatus = row[col]
	}
	return cell
}

func (p *Playback) commitObservation(vals map[string]any, ts time.Time, updates []playbackUpdate) error {
	written, err := p.store.writeLiveAndState(vals, ts, p.batch, updates)
	if err != nil {
		return err
	}
	changed := []string{}
	p.mu.Lock()
	for tag, raw := range vals {
		cell := raw.(replayValue)
		v, q, present := observation(cell.DAValue, cell.DAStatus)
		if !present {
			continue
		}
		p.current[tag] = Sample{Name: tag, TS: ts, Value: v, Quality: q, Origin: "live", BatchID: p.batch}
		changed = append(changed, tag)
	}
	p.lastCommit = ts
	p.lastDATags = len(changed)
	p.lastHDATags = len(written)
	p.lastExpectedTags = len(changed)
	notify := p.notify
	p.mu.Unlock()
	p.batch++
	if notify != nil {
		for _, tag := range changed {
			notify(tag)
		}
	}
	return nil
}
