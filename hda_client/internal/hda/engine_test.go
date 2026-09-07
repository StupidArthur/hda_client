package hda

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type runnerTestClient struct {
	mu        sync.Mutex
	calls     map[string]int
	results   map[string][]DataPoint
	err       map[string]error
	active    int
	maxActive int
}

func (c *runnerTestClient) ReadRaw(_ context.Context, nodeID string, _, _ time.Time) ([]DataPoint, error) {
	c.mu.Lock()
	c.calls[nodeID]++
	c.active++
	if c.active > c.maxActive {
		c.maxActive = c.active
	}
	c.mu.Unlock()

	time.Sleep(5 * time.Millisecond)

	c.mu.Lock()
	c.active--
	result := c.results[nodeID]
	err := c.err[nodeID]
	c.mu.Unlock()
	return result, err
}

func (*runnerTestClient) BrowseVariables(context.Context, uint16) ([]ServerTag, error) {
	return nil, nil
}

func (*runnerTestClient) Close() error { return nil }

func TestQueryRunnerQueriesEachNodeOnceAndSortsPoints(t *testing.T) {
	later := time.Date(2026, 9, 7, 10, 1, 0, 0, time.Local)
	earlier := later.Add(-time.Minute)
	client := &runnerTestClient{
		calls: make(map[string]int),
		results: map[string][]DataPoint{
			"ns=1;s=A": {{Time: later}, {Time: earlier}},
			"ns=1;s=B": {{Time: earlier}},
		},
		err: make(map[string]error),
	}

	got, err := QueryRunner(context.Background(), client, QueryConfig{
		URL: "opc.tcp://test", NS: 1, Tags: []string{"A", "B"}, EndTime: "2026-09-07T10:02:00",
		DurationSec: 120, Concurrency: 2,
	}, nil)
	if err != nil {
		t.Fatalf("QueryRunner() error = %v", err)
	}
	if client.calls["ns=1;s=A"] != 1 || client.calls["ns=1;s=B"] != 1 {
		t.Fatalf("calls = %#v, want exactly one call per node", client.calls)
	}
	if client.maxActive != 2 {
		t.Fatalf("max concurrency = %d, want 2", client.maxActive)
	}
	if len(got) != 2 || !got[0].Points[0].Time.Equal(earlier) || !got[0].Points[1].Time.Equal(later) {
		t.Fatalf("results are not ordered: %#v", got)
	}
}

func TestQueryRunnerReturnsNodeError(t *testing.T) {
	want := errors.New("read failed")
	client := &runnerTestClient{
		calls:   make(map[string]int),
		results: make(map[string][]DataPoint),
		err:     map[string]error{"ns=1;s=A": want},
	}

	_, err := QueryRunner(context.Background(), client, QueryConfig{
		URL: "opc.tcp://test", NS: 1, Tags: []string{"A"}, EndTime: "2026-09-07T10:02:00",
		DurationSec: 120, Concurrency: 1,
	}, nil)
	if !errors.Is(err, want) {
		t.Fatalf("QueryRunner() error = %v, want wrapped %v", err, want)
	}
}
