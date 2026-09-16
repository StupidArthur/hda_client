package main

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
)

func engineForTest(mode string) *HistoryEngine {
	s := Settings{Interval: time.Second, HistoryLength: time.Hour, QueryDuration: time.Hour, ReadTimeout: time.Second, ContinuationPointTTL: time.Minute, PageCap: 3, CPMode: mode}
	specs := map[string]NodeSpec{"dynamic_0001": {ID: "dynamic_0001", Kind: kindDynamic}}
	return newHistoryEngine(s, specs)
}

func rawDetails(start, end time.Time) *ua.ReadRawModifiedDetails {
	return &ua.ReadRawModifiedDetails{StartTime: start, EndTime: end, NumValuesPerNode: 10}
}

func valuesOf(t *testing.T, r *ua.HistoryReadResult) []*ua.DataValue {
	t.Helper()
	if r.StatusCode != ua.StatusOK {
		t.Fatalf("unexpected status: %s", r.StatusCode)
	}
	h, ok := r.HistoryData.Value.(*ua.HistoryData)
	if !ok {
		t.Fatalf("history data type = %T", r.HistoryData.Value)
	}
	return h.DataValues
}

func TestHistoryPageHonorsRangeCapAndRotatingContinuation(t *testing.T) {
	e := engineForTest("rotating")
	defer e.Close()
	end := time.Now().UTC().Add(-time.Second).Truncate(time.Second)
	start := end.Add(-7 * time.Second)
	node := &ua.HistoryReadValueID{NodeID: ua.NewStringNodeID(2, "dynamic_0001")}
	first := e.page("session-a", rawDetails(start, end), node, false)
	values := valuesOf(t, first)
	if len(values) != 3 || values[0].SourceTimestamp.Unix() != start.Unix() {
		t.Fatalf("unexpected first page: %d, %s", len(values), values[0].SourceTimestamp)
	}
	if len(first.ContinuationPoint) == 0 {
		t.Fatal("expected continuation point")
	}
	node.ContinuationPoint = first.ContinuationPoint
	second := e.page("session-a", rawDetails(start, end), node, false)
	values = valuesOf(t, second)
	if len(values) != 3 || values[0].SourceTimestamp.Unix() != start.Unix()+3 {
		t.Fatalf("cursor did not advance")
	}
	if string(first.ContinuationPoint) == string(second.ContinuationPoint) {
		t.Fatal("rotating continuation point did not rotate")
	}
}

func TestStableContinuationPointAdvancesAndCanRelease(t *testing.T) {
	e := engineForTest("stable")
	defer e.Close()
	end := time.Now().UTC().Add(-time.Second).Truncate(time.Second)
	start := end.Add(-7 * time.Second)
	node := &ua.HistoryReadValueID{NodeID: ua.NewStringNodeID(2, "dynamic_0001")}
	first := e.page("session-a", rawDetails(start, end), node, false)
	valuesOf(t, first)
	node.ContinuationPoint = first.ContinuationPoint
	second := e.page("session-a", rawDetails(start, end), node, false)
	values := valuesOf(t, second)
	if string(first.ContinuationPoint) != string(second.ContinuationPoint) {
		t.Fatal("stable continuation point changed")
	}
	if values[0].SourceTimestamp.Unix() != start.Unix()+3 {
		t.Fatal("stable token cursor did not advance")
	}
	released := e.page("session-a", rawDetails(start, end), node, true)
	if released.StatusCode != ua.StatusOK {
		t.Fatalf("release: %s", released.StatusCode)
	}
	if after := e.page("session-a", rawDetails(start, end), node, false); after.StatusCode != ua.StatusBadContinuationPointInvalid {
		t.Fatalf("after release = %s", after.StatusCode)
	}
}

func TestHistorySupportsReverseRanges(t *testing.T) {
	e := engineForTest("rotating")
	defer e.Close()
	start := time.Now().UTC().Add(-time.Second).Truncate(time.Second)
	end := start.Add(-7 * time.Second)
	r := e.page("session-a", rawDetails(start, end), &ua.HistoryReadValueID{NodeID: ua.NewStringNodeID(2, "dynamic_0001")}, false)
	v := valuesOf(t, r)
	if len(v) != 3 || v[1].SourceTimestamp.After(v[0].SourceTimestamp) {
		t.Fatal("reverse range was not generated descending")
	}
}

func TestQueryDurationContinuesForwardAndReverse(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		s := Settings{Interval: time.Second, HistoryLength: time.Hour, QueryDuration: 3 * time.Second, ReadTimeout: time.Second, ContinuationPointTTL: time.Minute, PageCap: 99, CPMode: "rotating"}
		e := newHistoryEngine(s, map[string]NodeSpec{"dynamic_0001": {ID: "dynamic_0001", Kind: kindDynamic}})
		end := time.Now().UTC().Add(-time.Second).Truncate(time.Second)
		start := end.Add(-9 * time.Second)
		if reverse {
			start, end = end, start
		}
		node := &ua.HistoryReadValueID{NodeID: ua.NewStringNodeID(2, "dynamic_0001")}
		var got []int64
		for {
			r := e.page("duration", rawDetails(start, end), node, false)
			for _, value := range valuesOf(t, r) {
				got = append(got, value.SourceTimestamp.Unix())
			}
			if len(r.ContinuationPoint) == 0 {
				break
			}
			node.ContinuationPoint = r.ContinuationPoint
		}
		e.Close()
		if len(got) != 10 {
			t.Fatalf("reverse=%v got %d points: %v", reverse, len(got), got)
		}
		for i := 1; i < len(got); i++ {
			if (!reverse && got[i] != got[i-1]+1) || (reverse && got[i] != got[i-1]-1) {
				t.Fatalf("reverse=%v gap/duplicate: %v", reverse, got)
			}
		}
	}
}

func TestArchiveOutsideRangeIsBadNoDataInBothDirections(t *testing.T) {
	e := engineForTest("rotating")
	defer e.Close()
	now := time.Now().UTC()
	cases := [][2]time.Time{
		{now.Add(-3 * time.Hour), now.Add(-2 * time.Hour)},
		{now.Add(-2 * time.Hour), now.Add(-3 * time.Hour)},
		{now.Add(2 * time.Hour), now.Add(3 * time.Hour)},
		{now.Add(3 * time.Hour), now.Add(2 * time.Hour)},
	}
	for _, tc := range cases {
		r := e.page("boundary", rawDetails(tc[0], tc[1]), &ua.HistoryReadValueID{NodeID: ua.NewStringNodeID(2, "dynamic_0001")}, false)
		if r.StatusCode != ua.StatusBadNoData {
			t.Fatalf("%s..%s: %s", tc[0], tc[1], r.StatusCode)
		}
	}
}

func TestStableContinuationSerializesDuplicateRequestsWithoutRepeats(t *testing.T) {
	s := Settings{Interval: time.Second, HistoryLength: time.Hour, QueryDuration: time.Hour, ReadTimeout: time.Second, ContinuationPointTTL: time.Minute, PageCap: 1, CPMode: "stable"}
	e := newHistoryEngine(s, map[string]NodeSpec{"dynamic_0001": {ID: "dynamic_0001", Kind: kindDynamic}})
	defer e.Close()
	end := time.Now().UTC().Add(-time.Second).Truncate(time.Second)
	start := end.Add(-30 * time.Second)
	initial := e.page("same", rawDetails(start, end), &ua.HistoryReadValueID{NodeID: ua.NewStringNodeID(2, "dynamic_0001")}, false)
	token := initial.ContinuationPoint
	var wg sync.WaitGroup
	got := make(chan int64, 12)
	errs := make(chan ua.StatusCode, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := e.page("same", rawDetails(start, end), &ua.HistoryReadValueID{NodeID: ua.NewStringNodeID(2, "dynamic_0001"), ContinuationPoint: token}, false)
			if r.StatusCode != ua.StatusOK {
				errs <- r.StatusCode
				return
			}
			got <- valuesOf(t, r)[0].SourceTimestamp.Unix()
		}()
	}
	wg.Wait()
	close(got)
	close(errs)
	for status := range errs {
		t.Fatalf("duplicate request status %s", status)
	}
	points := []int64{initial.HistoryData.Value.(*ua.HistoryData).DataValues[0].SourceTimestamp.Unix()}
	for ts := range got {
		points = append(points, ts)
	}
	sort.Slice(points, func(i, j int) bool { return points[i] < points[j] })
	for i := 1; i < len(points); i++ {
		if points[i] == points[i-1] {
			t.Fatalf("duplicate point %v", points)
		}
	}
}

func TestConcurrentCursorsAdvanceIndependently(t *testing.T) {
	s := Settings{Interval: time.Second, HistoryLength: time.Hour, QueryDuration: time.Hour, ReadTimeout: time.Second, ContinuationPointTTL: time.Minute, PageCap: 1, CPMode: "rotating"}
	specs := map[string]NodeSpec{}
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("dynamic_%04d", i)
		specs[id] = NodeSpec{ID: id, Kind: kindDynamic}
	}
	e := newHistoryEngine(s, specs)
	defer e.Close()
	end := time.Now().UTC().Add(-time.Second).Truncate(time.Second)
	start := end.Add(-19 * time.Second)
	var wg sync.WaitGroup
	errCh := make(chan error, 16)
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("dynamic_%04d", i)
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			node := &ua.HistoryReadValueID{NodeID: ua.NewStringNodeID(2, id)}
			seen := map[int64]bool{}
			for {
				r := e.page(id, rawDetails(start, end), node, false)
				if r.StatusCode != ua.StatusOK {
					errCh <- fmt.Errorf("%s: %s", id, r.StatusCode)
					return
				}
				for _, v := range valuesOf(t, r) {
					ts := v.SourceTimestamp.Unix()
					if seen[ts] {
						errCh <- fmt.Errorf("%s duplicate %d", id, ts)
						return
					}
					seen[ts] = true
				}
				if len(r.ContinuationPoint) == 0 {
					break
				}
				node.ContinuationPoint = r.ContinuationPoint
			}
			if len(seen) != 20 {
				errCh <- fmt.Errorf("%s got %d", id, len(seen))
			}
		}(id)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

func BenchmarkHistoryPage5000(b *testing.B) {
	s := Settings{Interval: 10 * time.Second, HistoryLength: 365 * 24 * time.Hour, QueryDuration: 365 * 24 * time.Hour, ReadTimeout: time.Second, ContinuationPointTTL: time.Minute, PageCap: 5000, CPMode: "rotating"}
	e := newHistoryEngine(s, map[string]NodeSpec{"dynamic_0001": {ID: "dynamic_0001", Kind: kindDynamic}})
	defer e.Close()
	end := time.Now().UTC().Add(-time.Second)
	start := end.Add(-24 * time.Hour)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		node := &ua.HistoryReadValueID{NodeID: ua.NewStringNodeID(2, "dynamic_0001")}
		details := rawDetails(start, end)
		details.NumValuesPerNode = 5000
		r := e.page("benchmark", details, node, false)
		if r.StatusCode != ua.StatusOK || len(r.ContinuationPoint) == 0 {
			b.Fatalf("bad result: %s", r.StatusCode)
		}
		e.page("benchmark", details, &ua.HistoryReadValueID{NodeID: node.NodeID, ContinuationPoint: r.ContinuationPoint}, true)
	}
}

func BenchmarkHistoryResponseEncode5000(b *testing.B) {
	s := Settings{Interval: 10 * time.Second, HistoryLength: 365 * 24 * time.Hour, QueryDuration: 365 * 24 * time.Hour, ReadTimeout: time.Second, ContinuationPointTTL: time.Minute, PageCap: 5000, CPMode: "rotating"}
	e := newHistoryEngine(s, map[string]NodeSpec{"dynamic_0001": {ID: "dynamic_0001", Kind: kindDynamic}})
	defer e.Close()
	end := time.Now().UTC().Add(-time.Second)
	details := rawDetails(end.Add(-24*time.Hour), end)
	details.NumValuesPerNode = 5000
	page := e.page("encode", details, &ua.HistoryReadValueID{NodeID: ua.NewStringNodeID(2, "dynamic_0001")}, false)
	response := &ua.HistoryReadResponse{ResponseHeader: response(1, ua.StatusOK), Results: []*ua.HistoryReadResult{page}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ua.Encode(response); err != nil {
			b.Fatal(err)
		}
	}
}
