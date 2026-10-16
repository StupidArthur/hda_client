package main

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

type Playback struct {
	store            *Store
	files            []FileInfo
	mu               sync.RWMutex
	current          map[string]Sample
	stop             chan struct{}
	done             chan struct{}
	batch            int64
	notify           func(string)
	started          bool
	closed           bool
	filesState       map[string]PlaybackFileState
	lastCommit       time.Time
	lastDATags       int
	lastHDATags      int
	lastExpectedTags int
}

// PlaybackFileState is a cheap in-memory snapshot for runtime reporting. It is updated
// only after the HDA write and cursor update commit together.
type PlaybackFileState struct {
	Name         string    `json:"name"`
	PeriodMS     int64     `json:"periodMs"`
	CurrentRow   int64     `json:"currentRow"`
	TotalRows    int64     `json:"totalRows"`
	LastCommit   time.Time `json:"lastCommit"`
	LastCommitMS int64     `json:"lastCommitMs"`
	Status       string    `json:"status"`
	LastError    string    `json:"lastError"`
}

func (p *Playback) SetNotify(f func(string)) { p.mu.Lock(); defer p.mu.Unlock(); p.notify = f }

func newPlayback(s *Store, files []FileInfo) *Playback {
	batch := int64(1)
	if s != nil {
		batch = s.lastBatch() + 1
	}
	states := make(map[string]PlaybackFileState, len(files))
	for _, f := range files {
		states[f.Path] = PlaybackFileState{Name: f.Name, PeriodMS: f.Period.Milliseconds(), TotalRows: f.Rows, Status: "等待首轮"}
	}
	return &Playback{store: s, files: files, current: map[string]Sample{}, stop: make(chan struct{}), done: make(chan struct{}), batch: batch, filesState: states}
}
func (p *Playback) Current(name string) (*Sample, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	x, ok := p.current[name]
	return &x, ok
}
func (p *Playback) Start() error {
	p.mu.RLock()
	started := p.started
	p.mu.RUnlock()
	if started {
		return nil
	}
	if len(p.files) == 0 {
		return nil
	}
	if err := p.validatePlaybackClock(time.Now().UTC()); err != nil {
		return err
	}
	for _, f := range p.files {
		if f.Timestamped {
			if len(p.files) != 1 {
				return fmt.Errorf("timestamp playback currently requires one DA file per preset")
			}
			return p.startTimestamped(f)
		}
	}
	if err := p.initializeFixed(); err != nil {
		return err
	}
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return nil
	}
	p.started = true
	p.mu.Unlock()
	go p.loop()
	return nil
}
func (p *Playback) Close() {
	p.mu.Lock()
	if len(p.files) == 0 || !p.started || p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()
	close(p.stop)
	<-p.done
}

func (p *Playback) Snapshot() []PlaybackFileState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]PlaybackFileState, 0, len(p.files))
	for _, f := range p.files {
		out = append(out, p.filesState[f.Path])
	}
	return out
}

func (p *Playback) DiagnosticSnapshot() (time.Time, int, int, int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastCommit, p.lastDATags, p.lastHDATags, p.lastExpectedTags
}
func (p *Playback) loop() {
	defer close(p.done)
	p.round(p.files)
	next := make(map[string]time.Time, len(p.files))
	now := time.Now()
	for _, f := range p.files {
		next[f.Path] = now.Add(f.Period)
	}
	for {
		var earliest time.Time
		for _, due := range next {
			if earliest.IsZero() || due.Before(earliest) {
				earliest = due
			}
		}
		timer := time.NewTimer(time.Until(earliest))
		select {
		case <-timer.C:
			now = time.Now()
			dueFiles := make([]FileInfo, 0, len(p.files))
			for _, f := range p.files {
				due := next[f.Path]
				if !due.After(now) {
					dueFiles = append(dueFiles, f)
					for !due.After(now) {
						due = due.Add(f.Period)
					}
					next[f.Path] = due
				}
			}
			p.round(dueFiles)
		case <-p.stop:
			if !timer.Stop() {
				<-timer.C
			}
			return
		}
	}
}
func (p *Playback) round(files []FileInfo) {
	timeNow := time.Now().UTC()
	vals := map[string]any{}
	next := map[string]int64{}
	shas := map[string]string{}
	for _, f := range files {
		sha, e := sha256File(f.Path)
		if e != nil {
			log.Printf("playback hash error file=%s err=%v", f.Path, e)
			return
		}
		row, e := p.store.playbackState(f.Path, sha)
		if e != nil {
			log.Printf("playback state error: %v", e)
			return
		}
		v, e := p.store.playbackRow(f, row)
		if e == sql.ErrNoRows {
			row = 0
			v, e = p.store.playbackRow(f, 0)
		}
		if e != nil {
			log.Printf("playback file=%s row=%d stopped: %v", f.Name, row, e)
			p.setFileError(f, e)
			continue
		}
		for _, n := range f.Tags {
			vals[n] = replayCell(f, v, n)
		}
		next[f.Path] = row + 1
		shas[f.Path] = sha
	}
	if len(vals) == 0 {
		return
	}
	updates := make([]playbackUpdate, 0, len(next))
	for path, row := range next {
		updates = append(updates, playbackUpdate{Path: path, SHA: shas[path], Next: row})
	}
	e := p.commitObservation(vals, timeNow, updates)
	if e != nil {
		log.Printf("playback transaction failed (batch=%d): %v", p.batch, e)
		for _, f := range files {
			p.failTimestampPlayback(f, e)
		}
		return
	}
	p.mu.Lock()
	for _, f := range files {
		state := p.filesState[f.Path]
		state.CurrentRow = next[f.Path]
		state.LastCommit = timeNow
		state.LastCommitMS = time.Since(timeNow).Milliseconds()
		state.Status = "运行中"
		state.LastError = ""
		p.filesState[f.Path] = state
	}
	p.mu.Unlock()
	log.Printf("playback committed batch=%d timestamp=%s", p.batch-1, timeNow.Format(time.RFC3339Nano))
}

func (p *Playback) setFileError(f FileInfo, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.filesState[f.Path]
	state.Status = "异常"
	state.LastError = err.Error()
	p.filesState[f.Path] = state
}
