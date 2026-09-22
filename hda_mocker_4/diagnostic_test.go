package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestDiagnosticContract(t *testing.T) {
	info := map[string]any{"summary": "test", "business.port": 4840, "enabled": true}
	diag := map[string]any{"status": "ok", "message": "DA 2/2", "opcua.session_count": 1}
	d := newDiagnosticServer("", func() (map[string]any, map[string]any) { return info, diag })
	ts := httptest.NewServer(d.handler())
	defer ts.Close()

	var compact map[string]any
	getJSON(t, ts.URL+"/v1/diag", &compact)
	assertFlatMap(t, compact)

	var detail map[string]map[string]any
	getJSON(t, ts.URL+"/v1/detail", &detail)
	if len(detail) != 2 || detail["info"] == nil || detail["diag"] == nil {
		t.Fatalf("detail must contain exactly info and diag: %#v", detail)
	}
	assertFlatMap(t, detail["info"])
	assertFlatMap(t, detail["diag"])
	if !reflect.DeepEqual(compact, detail["diag"]) {
		t.Fatalf("diag endpoints differ: %#v != %#v", compact, detail["diag"])
	}
}

func TestDiagnosticRoutes(t *testing.T) {
	d := newDiagnosticServer("", func() (map[string]any, map[string]any) {
		return map[string]any{}, map[string]any{"status": "ok", "message": "ready"}
	})
	ts := httptest.NewServer(d.handler())
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/diag", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%v err=%v", resp.StatusCode, err)
	}
	resp.Body.Close()
	resp, err = http.Get(ts.URL + "/unknown")
	if err != nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown status=%v err=%v", resp.StatusCode, err)
	}
	resp.Body.Close()
}

func getJSON(t *testing.T, url string, target any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d", url, resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type=%q", got)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func assertFlatMap(t *testing.T, value map[string]any) {
	t.Helper()
	for key, item := range value {
		switch item.(type) {
		case nil, string, float64, bool:
		default:
			t.Fatalf("%s has non-scalar value %T", key, item)
		}
	}
}
