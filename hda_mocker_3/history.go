package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopcua/opcua/ua"
)

type cursor struct {
	mu                         sync.Mutex
	NodeID                     string
	Next, SegmentEnd, FinalEnd int64
	Step                       int64
	Spec                       NodeSpec
	ExpiresAt                  time.Time
}
type HistoryEngine struct {
	settings Settings
	specs    map[string]NodeSpec
	// mu protects cps only. It is never held while a page's values are made.
	mu        sync.Mutex
	cps       map[string]map[string]*cursor
	nextToken atomic.Uint64
	stopSweep chan struct{}
	doneSweep chan struct{}
}

func newHistoryEngine(s Settings, specs map[string]NodeSpec) *HistoryEngine {
	if s.QueryDuration <= 0 {
		s.QueryDuration = s.HistoryLength
	}
	if s.ContinuationPointTTL <= 0 {
		s.ContinuationPointTTL = 5 * time.Minute
	}
	e := &HistoryEngine{settings: s, specs: specs, cps: make(map[string]map[string]*cursor), stopSweep: make(chan struct{}), doneSweep: make(chan struct{})}
	go e.sweepLoop()
	return e
}

func (e *HistoryEngine) Close() {
	select {
	case <-e.stopSweep:
		return
	default:
		close(e.stopSweep)
		<-e.doneSweep
	}
}
func (e *HistoryEngine) page(session string, details *ua.ReadRawModifiedDetails, item *ua.HistoryReadValueID, release bool) *ua.HistoryReadResult {
	if item == nil || item.NodeID == nil {
		return &ua.HistoryReadResult{StatusCode: ua.StatusBadNodeIDUnknown}
	}
	if release {
		return e.release(session, string(item.ContinuationPoint))
	}
	token := string(item.ContinuationPoint)
	var c *cursor
	if token != "" {
		var ok bool
		c, ok = e.acquire(session, token)
		if !ok {
			return &ua.HistoryReadResult{StatusCode: ua.StatusBadContinuationPointInvalid}
		}
		defer c.mu.Unlock()
	} else {
		spec, ok := e.specs[item.NodeID.StringID()]
		if !ok {
			return &ua.HistoryReadResult{StatusCode: ua.StatusBadNodeIDUnknown}
		}
		var hasData bool
		c, hasData = e.newCursor(item.NodeID.StringID(), spec, details.StartTime, details.EndTime)
		if !hasData {
			return &ua.HistoryReadResult{StatusCode: ua.StatusBadNoData}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
	}
	limit := e.settings.PageCap
	if details.NumValuesPerNode > 0 && details.NumValuesPerNode < limit {
		limit = details.NumValuesPerNode
	}
	remaining := uint64Distance(c.Next, c.SegmentEnd, c.Step)
	count := limit
	if remaining < uint64(count) {
		count = uint32(remaining)
	}
	values := make([]*ua.DataValue, count)
	deadline := time.Now().Add(e.settings.ReadTimeout)
	actualCount := count
	for i := uint32(0); i < count; i++ {
		// A page remains resumable if a huge request reaches its server-side
		// read budget. The check is deliberately outside the per-point hot path.
		if i != 0 && i&4095 == 0 && time.Now().After(deadline) {
			actualCount = i
			break
		}
		ts := c.Next + int64(i)*c.Step
		values[i] = &ua.DataValue{EncodingMask: ua.DataValueValue | ua.DataValueStatusCode | ua.DataValueSourceTimestamp, Value: ua.MustVariant(valueAt(c.Spec, ts)), Status: ua.StatusOK, SourceTimestamp: time.Unix(ts, 0).UTC()}
	}
	values = values[:actualCount]
	c.Next += int64(actualCount) * c.Step
	more := e.advanceSegment(c)
	result := &ua.HistoryReadResult{StatusCode: ua.StatusOK, HistoryData: ua.NewExtensionObject(&ua.HistoryData{DataValues: values})}
	if more {
		c.ExpiresAt = time.Now().Add(e.settings.ContinuationPointTTL)
		result.ContinuationPoint = e.put(session, token, c)
	} else if token != "" {
		e.removeIfCurrent(session, token, c)
	}
	return result
}

func (e *HistoryEngine) newCursor(node string, spec NodeSpec, start, end time.Time) (*cursor, bool) {
	now := time.Now().UTC()
	archiveStart, archiveEnd := now.Add(-e.settings.HistoryLength).Unix(), now.Unix()
	if start.IsZero() {
		start = time.Unix(archiveStart, 0)
	}
	if end.IsZero() {
		end = time.Unix(archiveEnd, 0)
	}
	a, b := start.UTC().Unix(), end.UTC().Unix()
	low, high := a, b
	if low > high {
		low, high = high, low
	}
	if high < archiveStart || low > archiveEnd {
		return nil, false
	}
	if a < archiveStart {
		a = archiveStart
	}
	if a > archiveEnd {
		a = archiveEnd
	}
	if b < archiveStart {
		b = archiveStart
	}
	if b > archiveEnd {
		b = archiveEnd
	}
	step := int64(e.settings.Interval / time.Second)
	if step < 1 {
		step = 1
	}
	if a > b {
		step = -step
	}
	c := &cursor{NodeID: node, Next: a, FinalEnd: b, Step: step, Spec: spec}
	c.SegmentEnd = segmentEnd(c.Next, c.FinalEnd, c.Step, int64(e.settings.QueryDuration/time.Second))
	return c, true
}

// hda_mocker_2 returns a continuation timestamp at the truncated segment end
// and continues it from +/- one second, including both range boundaries.
func (e *HistoryEngine) advanceSegment(c *cursor) bool {
	if c.Step > 0 {
		if c.Next <= c.SegmentEnd {
			return true
		}
		if c.SegmentEnd == c.FinalEnd {
			return false
		}
		c.Next = c.SegmentEnd + 1
	} else {
		if c.Next >= c.SegmentEnd {
			return true
		}
		if c.SegmentEnd == c.FinalEnd {
			return false
		}
		c.Next = c.SegmentEnd - 1
	}
	c.SegmentEnd = segmentEnd(c.Next, c.FinalEnd, c.Step, int64(e.settings.QueryDuration/time.Second))
	return true
}

func segmentEnd(start, final, step, duration int64) int64 {
	if duration < 1 {
		duration = 1
	}
	if step > 0 {
		end := start + duration
		if end > final {
			return final
		}
		return end
	}
	end := start - duration
	if end < final {
		return final
	}
	return end
}
func uint64Distance(next, end, step int64) uint64 {
	if step > 0 {
		if next > end {
			return 0
		}
		return uint64((end-next)/step + 1)
	}
	if next < end {
		return 0
	}
	return uint64((next-end)/(-step) + 1)
}

// acquire rechecks the map after obtaining c.mu. A duplicate request carrying
// a rotating token waits for the first request, then correctly becomes invalid
// when that first request replaces the token.
func (e *HistoryEngine) acquire(session, token string) (*cursor, bool) {
	e.mu.Lock()
	c := e.cps[session][token]
	e.mu.Unlock()
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	e.mu.Lock()
	current := e.cps[session][token]
	e.mu.Unlock()
	if current != c || (!c.ExpiresAt.IsZero() && time.Now().After(c.ExpiresAt)) {
		c.mu.Unlock()
		return nil, false
	}
	return c, true
}

func (e *HistoryEngine) put(session, previous string, c *cursor) []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cps[session] == nil {
		e.cps[session] = make(map[string]*cursor)
	}
	token := previous
	if token == "" || e.settings.CPMode == "rotating" {
		if previous != "" && e.cps[session][previous] == c {
			delete(e.cps[session], previous)
		}
		token = fmt.Sprintf("hda3-%x", e.nextToken.Add(1))
	}
	e.cps[session][token] = c
	return []byte(token)
}
func (e *HistoryEngine) removeIfCurrent(session, token string, c *cursor) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if m := e.cps[session]; m != nil && m[token] == c {
		delete(m, token)
		if len(m) == 0 {
			delete(e.cps, session)
		}
	}
}
func (e *HistoryEngine) release(session, token string) *ua.HistoryReadResult {
	if token == "" {
		return &ua.HistoryReadResult{StatusCode: ua.StatusOK}
	}
	c, ok := e.acquire(session, token)
	if !ok {
		return &ua.HistoryReadResult{StatusCode: ua.StatusBadContinuationPointInvalid}
	}
	e.removeIfCurrent(session, token, c)
	c.mu.Unlock()
	return &ua.HistoryReadResult{StatusCode: ua.StatusOK}
}

// gopcua's public server API does not expose a close-session hook. TTL bounds
// abandoned continuation points; cleanup runs in a ticker, never as a request
// path table scan.
func (e *HistoryEngine) sweepLoop() {
	interval := e.settings.ContinuationPointTTL / 2
	if interval > time.Minute || interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer close(e.doneSweep)
	for {
		select {
		case <-ticker.C:
			e.sweepExpired(time.Now())
		case <-e.stopSweep:
			return
		}
	}
}

func (e *HistoryEngine) sweepExpired(now time.Time) {
	type entry struct {
		session, token string
		c              *cursor
	}
	e.mu.Lock()
	entries := make([]entry, 0)
	for session, tokens := range e.cps {
		for token, c := range tokens {
			entries = append(entries, entry{session, token, c})
		}
	}
	e.mu.Unlock()
	for _, entry := range entries {
		entry.c.mu.Lock()
		if !entry.c.ExpiresAt.IsZero() && now.After(entry.c.ExpiresAt) {
			e.removeIfCurrent(entry.session, entry.token, entry.c)
		}
		entry.c.mu.Unlock()
	}
}
