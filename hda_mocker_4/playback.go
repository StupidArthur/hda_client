package main

import (
	"database/sql"
	"log"
	"sync"
	"time"
)

type Playback struct {
	store   *Store
	files   []FileInfo
	mu      sync.RWMutex
	current map[string]Sample
	stop    chan struct{}
	done    chan struct{}
	batch   int64
	notify  func(string)
}

func (p *Playback) SetNotify(f func(string)) { p.mu.Lock(); defer p.mu.Unlock(); p.notify = f }

func newPlayback(s *Store, files []FileInfo) *Playback {
	batch := int64(1)
	if s != nil {
		batch = s.lastBatch() + 1
	}
	return &Playback{store: s, files: files, current: map[string]Sample{}, stop: make(chan struct{}), done: make(chan struct{}), batch: batch}
}
func (p *Playback) Current(name string) (*Sample, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	x, ok := p.current[name]
	return &x, ok
}
func (p *Playback) Start() error {
	if len(p.files) == 0 {
		return nil
	}
	go p.loop()
	return nil
}
func (p *Playback) Close() {
	if len(p.files) == 0 {
		return
	}
	close(p.stop)
	<-p.done
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
			continue
		}
		for _, n := range f.Tags {
			vals[n] = v[n]
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
	written, e := p.store.writeLiveAndState(vals, timeNow, p.batch, updates)
	if e != nil {
		log.Printf("playback transaction failed (batch=%d): %v", p.batch, e)
		return
	}
	p.batch++
	p.mu.Lock()
	notify := p.notify
	for _, x := range written {
		p.current[x.Name] = x
	}
	p.mu.Unlock()
	if notify != nil {
		for _, x := range written {
			notify(x.Name)
		}
	}
	log.Printf("playback committed batch=%d tags=%d timestamp=%s", p.batch-1, len(written), timeNow.Format(time.RFC3339Nano))
}
