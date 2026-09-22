package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticClient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/diag" {
			fmt.Fprint(w, `{"status":"ok","message":"DA 2/2"}`)
			return
		}
		fmt.Fprint(w, `{"info":{"summary":"test"},"diag":{"status":"ok","message":"DA 2/2"}}`)
	}))
	defer ts.Close()
	endpoint := strings.TrimPrefix(ts.URL, "http://")
	c := NewDiagnosticClient(time.Second)
	diag, err := c.Diag(endpoint)
	if err != nil || diag["status"] != "ok" {
		t.Fatalf("diag=%v err=%v", diag, err)
	}
	detail, err := c.Detail(endpoint)
	if err != nil || detail["info"]["summary"] != "test" {
		t.Fatalf("detail=%v err=%v", detail, err)
	}
}

func TestAppFilesStayBesideExecutable(t *testing.T) {
	dir := t.TempDir()
	a, err := NewApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.configPath != dir+string([]rune{'\\'})+"lan_diag_manager.json" {
		t.Fatalf("config path=%s", a.configPath)
	}
	if err := a.SaveConfig(Config{PollSeconds: 2, TimeoutSeconds: 1, Tools: []ToolConfig{{Name: "mock", Host: "127.0.0.1", Port: 65534, Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	if a.historyPath != dir+string([]rune{'\\'})+"lan_diag_history.jsonl" {
		t.Fatalf("history path=%s", a.historyPath)
	}
}
