package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type diagnosticCollector func() (map[string]any, map[string]any)

type DiagnosticServer struct {
	listen   string
	server   *http.Server
	listener net.Listener
	collect  diagnosticCollector
}

func newDiagnosticServer(listen string, collect diagnosticCollector) *DiagnosticServer {
	return &DiagnosticServer{listen: listen, collect: collect}
}

func (d *DiagnosticServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/diag", d.serveDiag)
	mux.HandleFunc("/v1/detail", d.serveDetail)
	return mux
}

func (d *DiagnosticServer) Start() error {
	if d == nil || strings.TrimSpace(d.listen) == "" {
		return nil
	}
	ln, err := net.Listen("tcp", d.listen)
	if err != nil {
		return fmt.Errorf("start diagnostic listener %s: %w", d.listen, err)
	}
	d.listener = ln
	d.server = &http.Server{Handler: d.handler(), ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		if err := d.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("diagnostic server stopped unexpectedly: %v", err)
		}
	}()
	log.Printf("diagnostic service listening http://%s", d.listen)
	return nil
}

func (d *DiagnosticServer) Close() {
	if d == nil || d.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := d.server.Shutdown(ctx); err != nil {
		log.Printf("close diagnostic service: %v", err)
	}
}

func (d *DiagnosticServer) serveDiag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_, diag := d.collect()
	writeDiagnosticJSON(w, diag)
}

func (d *DiagnosticServer) serveDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	info, diag := d.collect()
	writeDiagnosticJSON(w, map[string]any{"info": info, "diag": diag})
}

func writeDiagnosticJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "encode diagnostic response", http.StatusInternalServerError)
	}
}

func (c *RuntimeController) diagnosticMaps() (map[string]any, map[string]any) {
	c.mu.RLock()
	snapshot := c.snapshot
	runtime := c.instance
	c.mu.RUnlock()

	info := map[string]any{
		"summary":              "OPC UA HDA预存、DA实时轮播及实时HDA入库模拟服务",
		"version":              version,
		"business.protocol":    "opc.tcp",
		"business.endpoint":    snapshot.Endpoint,
		"business.port":        endpointPort(snapshot.Endpoint),
		"tag.count":            snapshot.TagCount,
		"da.tag_count":         snapshot.DATagCount,
		"hda.file_count":       snapshot.HDAFilesTotal,
		"hda.imported_samples": snapshot.HDASamples,
	}
	diag := map[string]any{
		"status":                 "error",
		"message":                "OPC UA服务未运行",
		"runtime.phase":          snapshot.Phase,
		"da.last_time":           "",
		"da.tag_count":           0,
		"da.expected_count":      snapshot.DATagCount,
		"hda.last_time":          "",
		"hda.tag_count":          0,
		"hda.expected_count":     snapshot.DATagCount,
		"opcua.connection_count": 0,
		"opcua.session_count":    0,
	}
	if runtime == nil || runtime.playback == nil || runtime.server == nil || runtime.store == nil {
		if snapshot.Phase != phaseFailed && snapshot.Phase != phaseIdle {
			diag["status"] = "warn"
			diag["message"] = snapshot.Message
		}
		return info, diag
	}
	if tags, err := runtime.store.tags(); err == nil {
		info["tag.count"] = len(tags)
	}

	if start, end, err := runtime.store.timeRange("import"); err == nil {
		info["hda.native.start_time"], info["hda.native.end_time"] = formatDiagnosticTime(start), formatDiagnosticTime(end)
	}
	if start, end, err := runtime.store.timeRange("live"); err == nil {
		info["hda.live.start_time"], info["hda.live.end_time"] = formatDiagnosticTime(start), formatDiagnosticTime(end)
	}
	last, daCount, hdaCount, expectedCount := runtime.playback.DiagnosticSnapshot()
	diag["da.last_time"], diag["hda.last_time"] = formatDiagnosticTime(last), formatDiagnosticTime(last)
	diag["da.tag_count"], diag["hda.tag_count"] = daCount, hdaCount
	diag["da.expected_count"], diag["hda.expected_count"] = expectedCount, expectedCount
	connections := runtime.server.ConnectionAddresses()
	allSessions := runtime.server.SessionInfos()
	sessions := allSessions[:0]
	for _, session := range allSessions {
		if !session.ActivatedAt.IsZero() {
			sessions = append(sessions, session)
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].RemoteAddress+sessions[i].ApplicationURI+sessions[i].SessionName < sessions[j].RemoteAddress+sessions[j].ApplicationURI+sessions[j].SessionName
	})
	diag["opcua.connection_count"], diag["opcua.session_count"] = len(connections), len(sessions)
	for i, session := range sessions {
		prefix := "opcua.client." + strconv.Itoa(i+1) + "."
		diag[prefix+"remote_address"] = session.RemoteAddress
		diag[prefix+"application_name"] = session.ApplicationName
		diag[prefix+"application_uri"] = session.ApplicationURI
		diag[prefix+"product_uri"] = session.ProductURI
		diag[prefix+"session_name"] = session.SessionName
		diag[prefix+"created_at"] = formatDiagnosticTime(session.CreatedAt)
		diag[prefix+"activated_at"] = formatDiagnosticTime(session.ActivatedAt)
	}

	status := "ok"
	message := fmt.Sprintf("DA上一轮播放 %d/%d，时间=%s；HDA上一轮入库 %d/%d，时间=%s；UA会话 %d",
		daCount, expectedCount, formatDiagnosticTime(last), hdaCount, expectedCount, formatDiagnosticTime(last), len(sessions))
	if last.IsZero() {
		status = "warn"
		message = fmt.Sprintf("等待首轮DA播放和HDA入库；期望位号 %d；UA会话 %d", snapshot.DATagCount, len(sessions))
	} else if daCount < expectedCount || hdaCount < expectedCount {
		status = "warn"
	}
	diag["status"], diag["message"] = status, message
	return info, diag
}

func formatDiagnosticTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func endpointPort(endpoint string) int {
	u, err := url.Parse(endpoint)
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(u.Port())
	return port
}
