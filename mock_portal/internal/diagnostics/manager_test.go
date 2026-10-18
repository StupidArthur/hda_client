package diagnostics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mock_portal/internal/catalog"
)

func testConfig() Config {
	cfg := DefaultConfig()
	cfg.Interval = time.Hour
	cfg.Timeout = 300 * time.Millisecond
	return cfg
}

func mockWithTarget(url string) *catalog.Mock {
	return &catalog.Mock{
		ID:   "preset",
		UA:   catalog.Endpoint{Raw: "opc.tcp://10.0.0.5:18980", Valid: true, Host: "10.0.0.5", Port: 18980},
		Diag: catalog.DiagTarget{Listen: "10.0.0.5:4841", URL: url, Host: "10.0.0.5", Port: 4841, State: catalog.DiagMonitored},
	}
}

// collect mirrors what the background collector does for one preset.
func collect(manager *Manager, mock *catalog.Mock) {
	manager.ensure(mock)
	manager.fetchDiag(mock.ID, mock.Diag.URL)
}

func TestDiagSuccessAndReachableStatus(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/diag" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        "ok",
			"message":       "DA上一轮播放 3/3",
			"runtime.phase": "running",
		})
	}))
	defer server.Close()

	manager := New(testConfig())
	mock := mockWithTarget(server.URL)
	collect(manager, mock)

	status := manager.Status(mock)
	if status.State != StateOK {
		t.Fatalf("state = %s (%+v)", status.State, status)
	}
	if status.SourceStatus != "ok" || !strings.Contains(status.SourceMessage, "3/3") {
		t.Fatalf("source status not preserved: %+v", status)
	}
	if status.LastSuccess == "" || status.LastAttempt == "" {
		t.Fatalf("timestamps missing: %+v", status)
	}
	payload, at := manager.Diag(mock)
	if payload == nil || payload["runtime.phase"] != "running" || at.IsZero() {
		t.Fatalf("cached diag wrong: %v", payload)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("hits = %d", hits)
	}
}

func TestUnreachableAfterThreeFailuresAndRecovery(t *testing.T) {
	var fail int32 = 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&fail) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	manager := New(testConfig())
	mock := mockWithTarget(server.URL)
	for i := 0; i < 3; i++ {
		collect(manager, mock)
	}
	status := manager.Status(mock)
	if status.State != StateUnreachable {
		t.Fatalf("state = %s", status.State)
	}
	if status.ConsecutiveFailures != 3 || status.LastError == "" {
		t.Fatalf("failure bookkeeping wrong: %+v", status)
	}

	atomic.StoreInt32(&fail, 0)
	collect(manager, mock)
	status = manager.Status(mock)
	if status.State != StateOK || status.ConsecutiveFailures != 0 {
		t.Fatalf("recovery failed: %+v", status)
	}
}

func TestInvalidJSONAndTimeoutDegrade(t *testing.T) {
	manager := New(testConfig())
	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer badJSON.Close()
	mock := mockWithTarget(badJSON.URL)
	collect(manager, mock)
	status := manager.Status(mock)
	if status.State != StateDegraded {
		t.Fatalf("invalid JSON state = %s", status.State)
	}
	if !strings.Contains(status.LastError, "JSON") {
		t.Fatalf("unexpected error %q", status.LastError)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(800 * time.Millisecond)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer slow.Close()
	mock2 := mockWithTarget(slow.URL)
	collect(manager, mock2)
	if got := manager.Status(mock2); got.State != StateDegraded {
		t.Fatalf("timeout state = %s", got.State)
	}
}

func TestStaleAfterSilence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	manager := New(testConfig())
	base := time.Now()
	manager.now = func() time.Time { return base }
	mock := mockWithTarget(server.URL)
	collect(manager, mock)
	if got := manager.Status(mock); got.State != StateOK {
		t.Fatalf("state = %s", got.State)
	}
	manager.now = func() time.Time { return base.Add(45 * time.Second) }
	if got := manager.Status(mock); got.State != StateStale {
		t.Fatalf("expected stale, got %s", got.State)
	}
}

func TestUnmonitoredAndMisconfiguredStates(t *testing.T) {
	manager := New(testConfig())
	unmonitored := &catalog.Mock{ID: "a", Diag: catalog.DiagTarget{State: catalog.DiagUnmonitored, Reason: "未配置 diagnostic.listen"}}
	if got := manager.Status(unmonitored); got.State != StateUnmonitored || got.Reason == "" {
		t.Fatalf("unmonitored: %+v", got)
	}
	incomplete := &catalog.Mock{ID: "b", Diag: catalog.DiagTarget{State: catalog.DiagIncomplete, Reason: "通配地址"}}
	if got := manager.Status(incomplete); got.State != StateMisconfigured {
		t.Fatalf("incomplete: %+v", got)
	}
	pending := mockWithTarget("http://10.0.0.5:4841")
	if got := manager.Status(pending); got.State != StatePending {
		t.Fatalf("pending: %+v", got)
	}
}

func TestTargetChangeDiscardsOldResults(t *testing.T) {
	manager := New(testConfig())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	mock := mockWithTarget(server.URL)
	collect(manager, mock)
	if got := manager.Status(mock); got.State != StateOK {
		t.Fatalf("state = %s", got.State)
	}

	moved := mockWithTarget("http://10.0.0.9:4841")
	if got := manager.Status(moved); got.State != StatePending {
		t.Fatalf("address change kept stale health: %+v", got)
	}
}

func TestDetailCacheAndCoalescing(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"info": map[string]any{"version": "1.5.0", "tag.count": "76"},
			"diag": map[string]any{"status": "ok"},
		})
	}))
	defer server.Close()

	manager := New(testConfig())
	mock := mockWithTarget(server.URL)

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := manager.Detail(context.Background(), mock)
			if result.Info["version"] != "1.5.0" {
				t.Errorf("detail info = %v (%s)", result.Info, result.Error)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("detail hits = %d, want 1 (cache + coalescing)", got)
	}

	result := manager.Detail(context.Background(), mock)
	if result.FetchedAt == "" {
		t.Fatal("cached detail should keep its fetch time")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("detail hits after cache = %d", got)
	}
}

func TestDetailFailureKeepsLastGoodSnapshot(t *testing.T) {
	var fail int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&fail) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"info": map[string]any{"version": "1.5.0"},
			"diag": map[string]any{"status": "ok"},
		})
	}))
	defer server.Close()

	manager := New(testConfig())
	base := time.Now()
	manager.now = func() time.Time { return base }
	mock := mockWithTarget(server.URL)
	first := manager.Detail(context.Background(), mock)
	if first.Error != "" || first.Info["version"] != "1.5.0" {
		t.Fatalf("first detail: %+v", first)
	}

	atomic.StoreInt32(&fail, 1)
	manager.now = func() time.Time { return base.Add(30 * time.Second) }
	second := manager.Detail(context.Background(), mock)
	if second.Error == "" {
		t.Fatal("expected error after detail failure")
	}
	if second.Info["version"] != "1.5.0" {
		t.Fatalf("last good snapshot lost: %+v", second)
	}
}
