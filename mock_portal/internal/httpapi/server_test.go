package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mock_portal/internal/catalog"
	"mock_portal/internal/diagnostics"
	"mock_portal/internal/web"
)

const hdaContent = "0123456789"

func newDiagServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/diag":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":                                 "ok",
				"message":                                "DA上一轮播放 3/3，时间=2026-01-01T00:00:00Z；HDA上一轮入库 3/3",
				"runtime.phase":                          "running",
				"da.tag_count":                           3,
				"opcua.session_count":                    1,
				"opcua.notification_coalesced_total":     0,
				"opcua.notification_coalesced_last_time": "",
			})
		case "/v1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"info": map[string]any{
					"version":              "1.5.0",
					"business.endpoint":    "opc.tcp://127.0.0.1:18980",
					"tag.count":            "76",
					"hda.imported_samples": "1234",
				},
				"diag": map[string]any{"status": "ok", "message": "ok"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

type harness struct {
	handler http.Handler
	root    string
}

func newHarness(t *testing.T, diagHost string, diagPort int) *harness {
	t.Helper()
	root := t.TempDir()
	cfg := fmt.Sprintf(`server:
  endpoint: opc.tcp://127.0.0.1:18980
  ns: 3
  namespace: urn:hda:mocker4
diagnostic:
  listen: %s
playback:
  files:
    demo.parquet:
      period_ms: 30000
history:
  retention_days: 0
`, net.JoinHostPort(diagHost, fmt.Sprint(diagPort)))
	write(t, filepath.Join(root, "demo_preset", "config.yaml"), cfg)
	write(t, filepath.Join(root, "demo_preset", "summary.md"), "# Demo\n\n| a | b |\n|---|---|\n| 1 | 2 |\n")
	write(t, filepath.Join(root, "demo_preset", "hda", "demo.parquet"), hdaContent)
	write(t, filepath.Join(root, "demo_preset", "hda", "nested", "skip.parquet"), "SKIP")
	write(t, filepath.Join(root, "plain_material", "notes.md"), "not a preset")
	// Excluded by the default exclusion rule.
	write(t, filepath.Join(root, "demo", "config.yaml"), "server:\n  endpoint: opc.tcp://1.2.3.4:1\n")
	// Broken config must still be discoverable.
	write(t, filepath.Join(root, "broken", "config.yaml"), "server: [")

	cat, err := catalog.New(root, []string{"demo"})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	manager := diagnostics.New(diagnostics.Config{
		Interval:         time.Hour,
		Timeout:          time.Second,
		MaxConcurrent:    2,
		StaleAfter:       time.Minute,
		UnreachableAfter: 3,
		DetailTTL:        time.Second,
	})
	manager.CollectOnce(cat)
	server := New(cat, manager, web.Handler())
	return &harness{handler: server.Handler(), root: root}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMocksListCountsAndExclusions(t *testing.T) {
	diag := newDiagServer(t)
	defer diag.Close()
	host, port := hostPort(t, diag.URL)
	h := newHarness(t, host, port)

	rec := get(t, h.handler, "/api/mocks")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var payload struct {
		Total int `json:"total"`
		Mocks []struct {
			ID     string `json:"id"`
			Status struct {
				State        string `json:"state"`
				SourceStatus string `json:"source_status"`
			} `json:"status"`
			UA struct {
				Wildcard bool `json:"wildcard"`
				Valid    bool `json:"valid"`
			} `json:"ua"`
		} `json:"mocks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if payload.Total != 2 || len(payload.Mocks) != 2 {
		t.Fatalf("total = %d, mocks = %d", payload.Total, len(payload.Mocks))
	}
	ids := []string{payload.Mocks[0].ID, payload.Mocks[1].ID}
	if ids[0] != "broken" || ids[1] != "demo_preset" {
		t.Fatalf("ids = %v", ids)
	}
	if payload.Mocks[1].Status.State != "ok" || payload.Mocks[1].Status.SourceStatus != "ok" {
		t.Fatalf("healthy preset status = %+v", payload.Mocks[1].Status)
	}
	if payload.Mocks[0].Status.State == "ok" {
		t.Fatalf("broken preset should not look healthy: %+v", payload.Mocks[0].Status)
	}
}

func TestMockDetailIncludesConfigAndDetail(t *testing.T) {
	diag := newDiagServer(t)
	defer diag.Close()
	host, port := hostPort(t, diag.URL)
	h := newHarness(t, host, port)

	rec := get(t, h.handler, "/api/mocks/demo_preset")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(payload["config_text"].(string), "opc.tcp://127.0.0.1:18980") {
		t.Fatalf("config_text = %v", payload["config_text"])
	}
	detail := payload["detail"].(map[string]any)
	info := detail["info"].(map[string]any)
	if info["version"] != "1.5.0" {
		t.Fatalf("detail info = %v", info)
	}
	if payload["diagnostic"] == nil {
		t.Fatal("diagnostic cache missing")
	}
	if _, present := payload["endpoint_mismatch"]; present {
		t.Fatalf("endpoint should match, got %v", payload["endpoint_mismatch"])
	}

	missing := get(t, h.handler, "/api/mocks/does_not_exist")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing preset status = %d", missing.Code)
	}
}

func TestSummaryMissingAndPresent(t *testing.T) {
	diag := newDiagServer(t)
	defer diag.Close()
	host, port := hostPort(t, diag.URL)
	h := newHarness(t, host, port)

	rec := get(t, h.handler, "/api/mocks/demo_preset/summary")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "# Demo") {
		t.Fatalf("summary = %d %s", rec.Code, rec.Body.String())
	}
	broken := get(t, h.handler, "/api/mocks/broken/summary")
	if broken.Code != http.StatusOK || !strings.Contains(broken.Body.String(), "缺少 summary.md") {
		t.Fatalf("missing summary = %d %s", broken.Code, broken.Body.String())
	}
}

func TestFilesListingAndDownload(t *testing.T) {
	diag := newDiagServer(t)
	defer diag.Close()
	host, port := hostPort(t, diag.URL)
	h := newHarness(t, host, port)

	rec := get(t, h.handler, "/api/mocks/demo_preset/files")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var files struct {
		Files []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Category string `json:"category"`
			Size     int64  `json:"size"`
		} `json:"files"`
		HDACount int `json:"hda_count"`
		DAcount  int `json:"da_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &files); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if files.HDACount != 1 {
		t.Fatalf("hda_count = %d", files.HDACount)
	}
	var hdaID string
	for _, file := range files.Files {
		if strings.Contains(file.Name, "skip.parquet") {
			t.Fatal("nested parquet must not be listed")
		}
		if file.Category == "hda" {
			hdaID = file.ID
			if file.Size != int64(len(hdaContent)) {
				t.Fatalf("size = %d", file.Size)
			}
		}
	}
	if hdaID == "" {
		t.Fatal("hda file missing from listing")
	}

	download := get(t, h.handler, "/api/mocks/demo_preset/files/"+hdaID)
	if download.Code != http.StatusOK {
		t.Fatalf("download status = %d", download.Code)
	}
	if download.Body.String() != hdaContent {
		t.Fatalf("download body = %q", download.Body.String())
	}
	if disposition := download.Header().Get("Content-Disposition"); !strings.Contains(disposition, "demo.parquet") {
		t.Fatalf("disposition = %q", disposition)
	}
	if accept := download.Header().Get("Accept-Ranges"); accept != "bytes" {
		t.Fatalf("accept-ranges = %q", accept)
	}

	rangeReq := httptest.NewRequest(http.MethodGet, "/api/mocks/demo_preset/files/"+hdaID, nil)
	rangeReq.Header.Set("Range", "bytes=2-5")
	rangeRec := httptest.NewRecorder()
	h.handler.ServeHTTP(rangeRec, rangeReq)
	if rangeRec.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d", rangeRec.Code)
	}
	if rangeRec.Body.String() != "2345" {
		t.Fatalf("range body = %q", rangeRec.Body.String())
	}
}

func TestDownloadRejectsUnknownAndTraversalIDs(t *testing.T) {
	diag := newDiagServer(t)
	defer diag.Close()
	host, port := hostPort(t, diag.URL)
	h := newHarness(t, host, port)

	for _, id := range []string{"..%2Fconfig.yaml", "config", "unknown", "%2e%2e%2f%2e%2e%2fsecret"} {
		rec := get(t, h.handler, "/api/mocks/demo_preset/files/"+id)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
			t.Fatalf("id %q status = %d", id, rec.Code)
		}
	}
}

func TestReadOnlyMethods(t *testing.T) {
	diag := newDiagServer(t)
	defer diag.Close()
	host, port := hostPort(t, diag.URL)
	h := newHarness(t, host, port)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/mocks", strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d", method, rec.Code)
		}
	}
}

func TestStandardizationBundle(t *testing.T) {
	diag := newDiagServer(t)
	defer diag.Close()
	host, port := hostPort(t, diag.URL)
	h := newHarness(t, host, port)

	rec := get(t, h.handler, "/api/standardization")
	if rec.Code != http.StatusOK {
		t.Fatalf("guide status = %d", rec.Code)
	}
	var guide struct {
		Title    string `json:"title"`
		Sections []struct {
			Heading string `json:"heading"`
			Body    string `json:"body"`
		} `json:"sections"`
		Bundle struct {
			Name     string `json:"name"`
			URL      string `json:"url"`
			Size     int64  `json:"size"`
			Contents []struct {
				Path string `json:"path"`
			} `json:"contents"`
		} `json:"bundle"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &guide); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if guide.Title == "" || len(guide.Sections) < 3 {
		t.Fatalf("guide incomplete: %+v", guide)
	}
	if guide.Bundle.Size <= 0 || len(guide.Bundle.Contents) < 3 {
		t.Fatalf("bundle info incomplete: %+v", guide.Bundle)
	}
	found := false
	for _, item := range guide.Bundle.Contents {
		if item.Path == "skills/hda-data-import/SKILL.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("bundle must contain the skill entry point: %+v", guide.Bundle.Contents)
	}

	zipRec := get(t, h.handler, "/api/standardization/bundle")
	if zipRec.Code != http.StatusOK {
		t.Fatalf("bundle status = %d", zipRec.Code)
	}
	reader, err := zip.NewReader(bytes.NewReader(zipRec.Body.Bytes()), int64(zipRec.Body.Len()))
	if err != nil {
		t.Fatalf("bundle is not a zip: %v", err)
	}
	names := map[string]bool{}
	for _, file := range reader.File {
		names[file.Name] = true
	}
	for _, want := range []string{"skills/hda-data-import/SKILL.md", "tools/reformat_dataset.py", "examples/columns.md"} {
		if !names[want] {
			t.Fatalf("bundle missing %s (have %v)", want, names)
		}
	}
}

func TestSPAFallbackServesIndex(t *testing.T) {
	diag := newDiagServer(t)
	defer diag.Close()
	host, port := hostPort(t, diag.URL)
	h := newHarness(t, host, port)

	rec := get(t, h.handler, "/mocks/demo_preset")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("spa fallback = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "root") {
		t.Fatalf("spa body unexpected: %s", rec.Body.String())
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("csp = %q", csp)
	}
}

func TestEndpointMismatchIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/diag":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v1/detail":
			// Source reports a different address than the config.
			_, _ = io.WriteString(w, `{"info":{"business.endpoint":"opc.tcp://10.9.9.9:18980"},"diag":{"status":"ok"}}`)
		}
	}))
	defer server.Close()
	host, port := hostPort(t, server.URL)
	h := newHarness(t, host, port)

	rec := get(t, h.handler, "/api/mocks/demo_preset")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "endpoint_mismatch") {
		t.Fatalf("expected mismatch note: %s", rec.Body.String())
	}
}

func hostPort(t *testing.T, raw string) (string, int) {
	t.Helper()
	trimmed := strings.TrimPrefix(raw, "http://")
	host, portText, err := net.SplitHostPort(trimmed)
	if err != nil {
		t.Fatalf("split %s: %v", raw, err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatalf("port %s: %v", portText, err)
	}
	return host, port
}
