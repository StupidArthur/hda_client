package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/gopcua/opcua/ua"
)

type historyCursor struct {
	session          string
	name             string
	start, end, last time.Time
	forward          bool
	maxBatch         int64
	expires          time.Time
}
type HistoryEngine struct {
	store *Store
	cap   int
	nsID  uint16
	ttl   time.Duration
	mu    sync.Mutex
	cps   map[string]*historyCursor
	maxCP int
	stop  chan struct{}
	done  chan struct{}
}

func newHistory(s *Store, cap int, nsID uint16) *HistoryEngine {
	e := &HistoryEngine{store: s, cap: cap, nsID: nsID, ttl: 5 * time.Minute, maxCP: 1024, cps: map[string]*historyCursor{}, stop: make(chan struct{}), done: make(chan struct{})}
	go e.sweep()
	return e
}
func (e *HistoryEngine) Close()       { close(e.stop); <-e.done }
func (e *HistoryEngine) Active() bool { e.mu.Lock(); defer e.mu.Unlock(); return len(e.cps) > 0 }
func (e *HistoryEngine) ClearSession(session string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, c := range e.cps {
		if c.session == session {
			delete(e.cps, k)
		}
	}
}
func (e *HistoryEngine) sweep() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	defer close(e.done)
	for {
		select {
		case now := <-t.C:
			e.mu.Lock()
			for k, c := range e.cps {
				if now.After(c.expires) {
					delete(e.cps, k)
				}
			}
			e.mu.Unlock()
		case <-e.stop:
			return
		}
	}
}
func (e *HistoryEngine) page(session string, d *ua.ReadRawModifiedDetails, item *ua.HistoryReadValueID, release bool) *ua.HistoryReadResult {
	if item == nil || item.NodeID == nil {
		return &ua.HistoryReadResult{StatusCode: ua.StatusBadNodeIDUnknown}
	}
	if item.NodeID.Namespace() != e.nsID {
		return &ua.HistoryReadResult{StatusCode: ua.StatusBadNodeIDUnknown}
	}
	token := string(item.ContinuationPoint)
	if release {
		if token == "" {
			return &ua.HistoryReadResult{StatusCode: ua.StatusOK}
		}
		e.mu.Lock()
		c, ok := e.cps[token]
		if ok && (c.expires.Before(time.Now()) || c.session != session) {
			if c.expires.Before(time.Now()) {
				delete(e.cps, token)
			}
			ok = false
		}
		if ok {
			delete(e.cps, token)
		}
		e.mu.Unlock()
		if !ok {
			return &ua.HistoryReadResult{StatusCode: ua.StatusBadContinuationPointInvalid}
		}
		return &ua.HistoryReadResult{StatusCode: ua.StatusOK}
	}
	var c *historyCursor
	if token != "" {
		e.mu.Lock()
		c = e.cps[token]
		if c != nil && c.expires.Before(time.Now()) {
			delete(e.cps, token)
			c = nil
		}
		e.mu.Unlock()
		if c == nil || c.session != session || c.name != item.NodeID.StringID() {
			return &ua.HistoryReadResult{StatusCode: ua.StatusBadContinuationPointInvalid}
		}
	} else {
		name := item.NodeID.StringID()
		if _, err := e.store.latest(name); err != nil {
			return &ua.HistoryReadResult{StatusCode: ua.StatusBadNodeIDUnknown}
		}
		start, end := d.StartTime.UTC(), d.EndTime.UTC()
		forward := true
		if start.IsZero() {
			start = time.Unix(0, 0).UTC()
		}
		if end.IsZero() {
			end = time.Now().UTC()
		}
		if start.After(end) {
			start, end = end, start
			forward = false
		}
		c = &historyCursor{session: session, name: name, start: start, end: end, forward: forward, maxBatch: e.store.lastBatch(), expires: time.Now().Add(e.ttl)}
	}
	limit := e.cap
	if d.NumValuesPerNode > 0 && int(d.NumValuesPerNode) < limit {
		limit = int(d.NumValuesPerNode)
	}
	var after *time.Time
	if !c.last.IsZero() {
		x := c.last
		after = &x
	}
	vals, err := e.store.samplesBound(c.name, c.start, c.end, c.forward, after, limit, c.maxBatch)
	if err != nil {
		return &ua.HistoryReadResult{StatusCode: ua.StatusBadInternalError}
	}
	if len(vals) == 0 {
		return &ua.HistoryReadResult{StatusCode: ua.StatusBadNoData}
	}
	data := make([]*ua.DataValue, len(vals))
	for i, x := range vals {
		data[i] = dataValueFromSample(x)
	}
	c.last = vals[len(vals)-1].TS
	// Determine whether another row exists without materialising it.
	more, err := e.store.hasSamplesAfter(c.name, c.start, c.end, c.forward, &c.last, c.maxBatch)
	if err != nil {
		return &ua.HistoryReadResult{StatusCode: ua.StatusBadInternalError}
	}
	res := &ua.HistoryReadResult{StatusCode: ua.StatusOK, HistoryData: ua.NewExtensionObject(&ua.HistoryData{DataValues: data})}
	if more {
		if token == "" {
			e.mu.Lock()
			tooMany := len(e.cps) >= e.maxCP
			e.mu.Unlock()
			if tooMany {
				return &ua.HistoryReadResult{StatusCode: ua.StatusBadTooManyOperations}
			}
			buf := make([]byte, 24)
			if _, re := rand.Read(buf); re != nil {
				return &ua.HistoryReadResult{StatusCode: ua.StatusBadInternalError}
			}
			token = "hda4-" + hex.EncodeToString(buf)
		}
		c.expires = time.Now().Add(e.ttl)
		e.mu.Lock()
		e.cps[token] = c
		e.mu.Unlock()
		res.ContinuationPoint = []byte(token)
	} else if token != "" {
		e.mu.Lock()
		delete(e.cps, token)
		e.mu.Unlock()
	}
	return res
}

func dataValueFromSample(x Sample) *ua.DataValue {
	mask := byte(ua.DataValueStatusCode | ua.DataValueSourceTimestamp)
	dv := &ua.DataValue{EncodingMask: mask, Status: ua.StatusCode(x.Quality), SourceTimestamp: x.TS}
	if x.Value != nil {
		dv.EncodingMask |= ua.DataValueValue
		dv.Value = ua.MustVariant(*x.Value)
	}
	return dv
}
