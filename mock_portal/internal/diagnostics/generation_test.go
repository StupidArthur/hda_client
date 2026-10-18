package diagnostics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInFlightResultsCannotCrossTargetGeneration(t *testing.T) {
	for _, detail := range []bool{false, true} {
		for _, aba := range []bool{false, true} {
			t.Run(fmt.Sprintf("detail=%v/ABA=%v", detail, aba), func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					close(entered)
					<-release
					if detail {
						fmt.Fprint(w, `{"info":{"version":"OLD"},"diag":{"message":"OLD"}}`)
					} else {
						fmt.Fprint(w, `{"status":"ok","message":"OLD"}`)
					}
				}))
				defer server.Close()
				manager := New(testConfig())
				old := mockWithTarget(server.URL)
				manager.ensure(old)
				done := make(chan DetailResult, 1)
				go func() {
					if detail {
						done <- manager.Detail(context.Background(), old)
					} else {
						manager.fetchDiag(old.ID, old.Diag.URL)
						done <- DetailResult{}
					}
				}()
				<-entered
				current := mockWithTarget("http://127.0.0.1:1")
				manager.ensure(current)
				if aba {
					current = old
					manager.ensure(current)
				}
				close(release)
				result := <-done
				if detail && (result.Error == "" || result.Info != nil) {
					t.Fatalf("old detail was published: %+v", result)
				}
				if status := manager.Status(current); status.SourceMessage != "" || status.State != StatePending {
					t.Fatalf("old diagnostic contaminated new target: %+v", status)
				}
			})
		}
	}
}

func TestOldDetailCompletionPreservesNewInFlightRequest(t *testing.T) {
	oldEntered, oldRelease := make(chan struct{}), make(chan struct{})
	newEntered, newRelease := make(chan struct{}), make(chan struct{})
	server := func(entered, release chan struct{}, version string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-release
			fmt.Fprintf(w, `{"info":{"version":%q}}`, version)
		}))
	}
	oldServer := server(oldEntered, oldRelease, "OLD")
	defer oldServer.Close()
	newServer := server(newEntered, newRelease, "NEW")
	defer newServer.Close()
	manager := New(testConfig())
	oldDone, newDone := make(chan DetailResult, 1), make(chan DetailResult, 1)
	go func() { oldDone <- manager.Detail(context.Background(), mockWithTarget(oldServer.URL)) }()
	<-oldEntered
	current := mockWithTarget(newServer.URL)
	go func() { newDone <- manager.Detail(context.Background(), current) }()
	<-newEntered
	close(oldRelease)
	<-oldDone
	manager.mu.Lock()
	busy := manager.states[current.ID].detailBusy
	manager.mu.Unlock()
	close(newRelease)
	result := <-newDone
	if !busy || result.Error != "" || result.Info["version"] != "NEW" {
		t.Fatalf("new request was overwritten: busy=%v result=%+v", busy, result)
	}
	if cached := manager.Detail(context.Background(), current); cached.Info["version"] != "NEW" {
		t.Fatalf("new cache was lost: %+v", cached)
	}
}
