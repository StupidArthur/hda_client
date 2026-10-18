// Command fake_diag is a local test fixture: it serves the same /v1/diag and
// /v1/detail shapes as hda_mocker_4 so the portal can be exercised without a
// running Mock. It is never part of the portal runtime.
//
//	go run ./scripts/fake_diag -listen 127.0.0.1:18990 -status ok
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18990", "HTTP listen address")
	status := flag.String("status", "ok", "source status: ok | warn | error")
	phase := flag.String("phase", "running", "runtime.phase value")
	flag.Parse()

	message := map[string]string{
		"ok":    "DA上一轮播放 3/3，时间=" + time.Now().UTC().Format(time.RFC3339Nano) + "；HDA上一轮入库 3/3",
		"warn":  "等待首轮DA播放和HDA入库；期望位号 3；UA会话 0",
		"error": "playback error: 数据文件读取失败",
	}[*status]
	if message == "" {
		message = "fixture status " + *status
	}

	diag := map[string]any{
		"status":                                 *status,
		"message":                                message,
		"runtime.phase":                          *phase,
		"da.last_time":                           time.Now().UTC().Format(time.RFC3339Nano),
		"da.tag_count":                           3,
		"da.expected_count":                      3,
		"hda.last_time":                          time.Now().UTC().Format(time.RFC3339Nano),
		"hda.tag_count":                          3,
		"hda.expected_count":                     3,
		"opcua.connection_count":                 1,
		"opcua.session_count":                    1,
		"opcua.subscription_count":               1,
		"opcua.monitored_item_count":             3,
		"opcua.notification_pending":             0,
		"opcua.notification_coalesced_total":     uint64(0),
		"opcua.notification_coalesced_last_time": "",
	}
	info := map[string]any{
		"summary":               "fixture",
		"version":               "1.5.0-fixture",
		"business.protocol":     "opc.tcp",
		"business.endpoint":     "opc.tcp://127.0.0.1:18980",
		"business.port":         18980,
		"tag.count":             3,
		"da.tag_count":          3,
		"hda.file_count":        1,
		"hda.imported_samples":  1440,
		"hda.native.start_time": "2026-01-01T00:00:00Z",
		"hda.native.end_time":   "2026-01-01T01:00:00Z",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/diag", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, diag)
	})
	mux.HandleFunc("/v1/detail", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"info": info, "diag": diag})
	})
	log.Printf("fake diagnostic fixture listening http://%s (status=%s)", *listen, *status)
	if err := http.ListenAndServe(*listen, mux); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}
