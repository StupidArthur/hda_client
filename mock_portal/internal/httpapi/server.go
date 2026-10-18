package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"

	"mock_portal/internal/catalog"
	"mock_portal/internal/diagnostics"
)

// Server exposes the read-only portal API and the embedded SPA.
type Server struct {
	Catalog *catalog.Catalog
	Diag    *diagnostics.Manager
	Web     http.Handler
	Now     func() time.Time
}

func New(cat *catalog.Catalog, diag *diagnostics.Manager, web http.Handler) *Server {
	return &Server{Catalog: cat, Diag: diag, Web: web, Now: time.Now}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/mocks", s.handleMocks)
	mux.HandleFunc("GET /api/mocks/{id}", s.handleMock)
	mux.HandleFunc("GET /api/mocks/{id}/summary", s.handleSummary)
	mux.HandleFunc("GET /api/mocks/{id}/files", s.handleFiles)
	mux.HandleFunc("GET /api/mocks/{id}/files/{fileId}", s.handleDownload)
	mux.HandleFunc("GET /api/standardization", s.handleStandardization)
	mux.HandleFunc("GET /api/standardization/bundle", s.handleBundle)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("/", s.Web)
	return s.middleware(mux)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("portal panic on %s %s: %v", r.Method, r.URL.Path, recovered)
				writeError(w, http.StatusInternalServerError, "门户内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type mockView struct {
	*catalog.Mock
	Status diagnostics.Status `json:"status"`
}

type listResponse struct {
	Total     int        `json:"total"`
	ScannedAt string     `json:"scanned_at"`
	Stale     bool       `json:"stale"`
	Error     string     `json:"error,omitempty"`
	Mocks     []mockView `json:"mocks"`
}

func (s *Server) view(mock *catalog.Mock) mockView {
	return mockView{Mock: mock, Status: s.Diag.Status(mock)}
}

func (s *Server) handleMocks(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") == "1"
	snap := s.Catalog.Snapshot(force)
	views := make([]mockView, 0, len(snap.Mocks))
	for _, mock := range snap.Mocks {
		views = append(views, s.view(mock))
	}
	writeJSON(w, http.StatusOK, listResponse{
		Total:     len(views),
		ScannedAt: formatTime(snap.ScannedAt),
		Stale:     snap.Stale,
		Error:     snap.Error,
		Mocks:     views,
	})
}

type detailResponse struct {
	mockView
	ConfigText          string                   `json:"config_text"`
	Diagnostic          map[string]any           `json:"diagnostic"`
	DiagnosticFetchedAt string                   `json:"diagnostic_fetched_at,omitempty"`
	Detail              diagnostics.DetailResult `json:"detail"`
	EndpointMismatch    string                   `json:"endpoint_mismatch,omitempty"`
}

func (s *Server) handleMock(w http.ResponseWriter, r *http.Request) {
	mock := s.lookup(w, r)
	if mock == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	detail := s.Diag.Detail(ctx, mock)
	diagPayload, diagAt := s.Diag.Diag(mock)
	response := detailResponse{
		mockView:            s.view(mock),
		ConfigText:          mock.ConfigText(),
		Diagnostic:          diagPayload,
		DiagnosticFetchedAt: formatTime(diagAt),
		Detail:              detail,
		EndpointMismatch:    endpointMismatch(mock, detail.Info),
	}
	writeJSON(w, http.StatusOK, response)
}

func endpointMismatch(mock *catalog.Mock, info map[string]any) string {
	if !mock.UA.Valid || info == nil {
		return ""
	}
	actual, _ := info["business.endpoint"].(string)
	actual = strings.TrimSpace(actual)
	configured := strings.TrimSpace(mock.UA.Raw)
	if actual == "" || configured == "" || strings.EqualFold(actual, configured) {
		return ""
	}
	return "config 中的 server.endpoint 与源端 business.endpoint 不一致；页面连接地址仍以当前 config 为准"
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	mock := s.lookup(w, r)
	if mock == nil {
		return
	}
	markdown, modified, err := mock.Summary()
	if err != nil {
		if errors.Is(err, catalog.ErrFileNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{
				"id": mock.ID, "markdown": "", "error": "缺少 summary.md",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": mock.ID, "markdown": "", "error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          mock.ID,
		"markdown":    markdown,
		"modified_at": formatTime(modified),
		"size":        len(markdown),
	})
}

type fileView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	mock := s.lookup(w, r)
	if mock == nil {
		return
	}
	files := mock.ListFiles()
	views := make([]fileView, 0, len(files))
	for _, file := range files {
		views = append(views, fileView{
			ID:         file.ID,
			Name:       file.Name,
			Category:   file.Category,
			Size:       file.Size,
			ModifiedAt: formatTime(file.ModifiedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        mock.ID,
		"files":     views,
		"hda_count": mock.HDACount,
		"da_count":  mock.DACount,
	})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	mock := s.lookup(w, r)
	if mock == nil {
		return
	}
	file, meta, err := mock.OpenFile(r.PathValue("fileId"))
	if err != nil {
		if errors.Is(err, catalog.ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "文件不存在或不在允许下载的范围内")
			return
		}
		writeError(w, http.StatusForbidden, "文件不在允许下载的范围内")
		return
	}
	defer file.Close()
	contentType := "application/octet-stream"
	switch meta.Category {
	case catalog.CategoryConfig:
		contentType = "text/yaml; charset=utf-8"
	case catalog.CategorySummary:
		contentType = "text/markdown; charset=utf-8"
	case catalog.CategoryHDA, catalog.CategoryDA:
		contentType = "application/vnd.apache.parquet"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": meta.Name}))
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, meta.Name, meta.ModifiedAt, file)
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) *catalog.Mock {
	id := r.PathValue("id")
	snap := s.Catalog.Snapshot(false)
	mock := snap.Find(id)
	if mock == nil {
		writeError(w, http.StatusNotFound, "未找到 Mock: "+id)
		return nil
	}
	return mock
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
