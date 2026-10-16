package main

import (
	"database/sql"
	"fmt"
	"github.com/gopcua/opcua/ua"
	"log"
	"strings"
	"time"
)

type timedObservation struct {
	at     time.Time
	values map[string]any
}

func (p *Playback) startTimestamped(f FileInfo) error {
	cols := []string{quoteIdent("Timestamp")}
	for _, tag := range f.Tags {
		cols = append(cols, quoteIdent(tag))
	}
	for _, m := range []map[string]string{f.StatusCols, f.HDAValueCols, f.HDAStatusCols} {
		for _, col := range m {
			cols = append(cols, quoteIdent(col))
		}
	}
	rows, err := p.store.db.Query("SELECT "+strings.Join(cols, ",")+" FROM read_parquet(?)", f.Path)
	if err != nil {
		return err
	}
	defer rows.Close()
	observations := []timedObservation{}
	last := map[string]Sample{}
	for rows.Next() {
		cells := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range cells {
			dest[i] = &cells[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		at, err := normalizeTime(cells[0])
		if err != nil {
			return err
		}
		if len(observations) > 0 && !at.After(observations[len(observations)-1].at) {
			return fmt.Errorf("DA timestamps must be strictly increasing")
		}
		row := map[string]any{}
		names, err := rows.Columns()
		if err != nil {
			return err
		}
		for i, name := range names {
			row[name] = cells[i]
		}
		obs := timedObservation{at: at, values: map[string]any{}}
		for _, tag := range f.Tags {
			cell := replayCell(f, row, tag)
			obs.values[tag] = cell
			if v, q, present := observation(cell.DAValue, cell.DAStatus); present {
				last[tag] = Sample{Name: tag, Value: v, Quality: q, Origin: "replay"}
			}
		}
		observations = append(observations, obs)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(observations) == 0 {
		return fmt.Errorf("empty timestamp DA")
	}
	for _, tag := range f.Tags {
		if _, ok := last[tag]; !ok {
			last[tag] = Sample{Name: tag, Quality: uint32(ua.StatusBadWaitingForInitialData), Origin: "replay"}
		}
	}
	if err := p.validatePlaybackClock(time.Now().UTC()); err != nil {
		return err
	}
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = true
	now := time.Now().UTC()
	for tag, v := range last {
		v.TS = now
		p.current[tag] = v
	}
	p.mu.Unlock()
	go p.timestampLoop(f, observations)
	return nil
}

func (p *Playback) timestampLoop(f FileInfo, observations []timedObservation) {
	defer close(p.done)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	base := time.Now()
	index := 0
	duration := observations[len(observations)-1].at.Sub(observations[0].at) + 100*time.Millisecond
	for {
		now := time.Now()
		for !base.Add(observations[index].at.Sub(observations[0].at)).After(now) {
			select {
			case <-p.stop:
				return
			default:
			}
			obs := observations[index]
			scheduled := base.Add(obs.at.Sub(observations[0].at)).UTC()
			if err := p.commitObservation(obs.values, scheduled, nil); err != nil {
				p.failTimestampPlayback(f, err)
				log.Printf("timestamp playback stopped before publishing: %v", err)
				return
			}
			p.mu.Lock()
			state := p.filesState[f.Path]
			state.CurrentRow = int64(index + 1)
			state.LastCommit = scheduled
			state.Status = "running"
			p.filesState[f.Path] = state
			p.mu.Unlock()
			index++
			if index == len(observations) {
				index = 0
				base = base.Add(duration)
			}
		}
		select {
		case <-p.stop:
			return
		case <-ticker.C:
		}
	}
}

func (p *Playback) validatePlaybackClock(now time.Time) error {
	var latest sql.NullInt64
	if err := p.store.db.QueryRow("SELECT max(ts) FROM samples").Scan(&latest); err != nil {
		return err
	}
	if latest.Valid && now.UnixMicro() <= latest.Int64 {
		return fmt.Errorf("timestamp playback cannot start: current UTC %s must be later than existing history %s; check source timezone/system clock or restart after history end; source timestamps have NOT been shifted", now.Format(time.RFC3339Nano), time.UnixMicro(latest.Int64).UTC().Format(time.RFC3339Nano))
	}
	return nil
}

func (p *Playback) failTimestampPlayback(f FileInfo, err error) {
	p.setFileError(f, err)
	p.mu.Lock()
	for tag, x := range p.current {
		x.Quality = uint32(ua.StatusBadOutOfService)
		p.current[tag] = x
	}
	notify := p.notify
	p.mu.Unlock()
	if notify != nil {
		for _, tag := range f.Tags {
			notify(tag)
		}
	}
}
