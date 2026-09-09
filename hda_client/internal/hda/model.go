package hda

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultConcurrency = 16
	maxConcurrency     = 128
	queryTimeLayout    = "2006-01-02T15:04:05"
)

// QueryConfig 一次查询的完整配置。
type QueryConfig struct {
	URL         string   `json:"url"`
	NS          uint16   `json:"ns"`
	Tags        []string `json:"tags"`
	EndTime     string   `json:"end_time"` // RFC3339 本地时间, 如 2026-09-04T10:00:00
	DurationSec int64    `json:"duration_sec"`
	PageSize    uint32   `json:"page_size"`
	Concurrency int      `json:"concurrency"`
}

// NormalizeAndValidate returns the canonical form used for persistence and execution.
func (c QueryConfig) NormalizeAndValidate() (QueryConfig, error) {
	c.URL = strings.TrimSpace(c.URL)
	if c.URL == "" {
		return QueryConfig{}, fmt.Errorf("URL 不能为空")
	}
	if c.DurationSec <= 0 {
		return QueryConfig{}, fmt.Errorf("查询时长必须大于 0 秒")
	}
	if _, err := time.ParseInLocation(queryTimeLayout, c.EndTime, time.Local); err != nil {
		return QueryConfig{}, fmt.Errorf("结束时间格式错误(需 yyyy-MM-ddTHH:mm:ss): %w", err)
	}
	if c.Concurrency == 0 {
		c.Concurrency = defaultConcurrency
	}
	if c.PageSize == 0 {
		c.PageSize = 5000
	}
	if c.PageSize > 1000000 {
		return QueryConfig{}, fmt.Errorf("单页上限必须在 1-1000000 之间")
	}
	if c.Concurrency < 1 || c.Concurrency > maxConcurrency {
		return QueryConfig{}, fmt.Errorf("并发数必须在 1-%d 之间", maxConcurrency)
	}

	seen := make(map[string]struct{}, len(c.Tags))
	tags := make([]string, 0, len(c.Tags))
	for _, raw := range c.Tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	if len(tags) == 0 {
		return QueryConfig{}, fmt.Errorf("至少需要一个位号")
	}
	c.Tags = tags
	return c, nil
}

// DataPoint 一条历史数据。
type DataPoint struct {
	Time     time.Time `json:"time"`
	Value    float64   `json:"value"`
	Quality  string    `json:"quality"`
	HasValue bool      `json:"has_value"`
}

// TagResult 单个位号的查询结果。
type TagResult struct {
	Tag    string      `json:"tag"`
	Points []DataPoint `json:"points"`
}

// QueryProgress 进度。
type QueryProgress struct {
	Done     int   `json:"done"`     // 已完成的段
	Total    int   `json:"total"`    // 总段数
	Records  int64 `json:"records"`  // 已累计记录数
	Active   bool  `json:"active"`   // 是否运行中
	Canceled bool  `json:"canceled"` // 是否被取消
}

// ParquetRow is the stable, long-table representation written by the desktop
// client. Timestamp is UTC unix nanoseconds so files do not depend on the
// computer's locale when they are opened later.
type ParquetRow struct {
	Timestamp int64   `parquet:"timestamp" json:"timestamp"`
	Node      string  `parquet:"node" json:"node"`
	Value     float64 `parquet:"value" json:"value"`
	Quality   string  `parquet:"quality" json:"quality"`
	HasValue  bool    `parquet:"has_value" json:"has_value"`
}

// ParquetResult is deliberately small enough to cross the Wails bridge after
// a query; history points themselves remain in the parquet file.
type ParquetResult struct {
	Path      string  `json:"path"`
	Nodes     int     `json:"nodes"`
	Records   int64   `json:"records"`
	Good      int64   `json:"good"`
	Bad       int64   `json:"bad"`
	Uncertain int64   `json:"uncertain"`
	NoValue   int64   `json:"no_value"`
	ElapsedMS int64   `json:"elapsed_ms"`
	Rate      float64 `json:"rate"`
}

type ParquetPage struct {
	Rows  []ParquetViewRow `json:"rows"`
	Total int64            `json:"total"`
	Nodes []string         `json:"nodes"`
}

// ParquetViewRow is a bridge DTO. Parquet retains nanoseconds as int64, but a
// JavaScript number cannot represent UnixNano safely, so time crosses Wails as
// RFC3339Nano text.
type ParquetViewRow struct {
	Timestamp string  `json:"timestamp"`
	Node      string  `json:"node"`
	Value     float64 `json:"value"`
	Quality   string  `json:"quality"`
	HasValue  bool    `json:"has_value"`
}

type ParquetSummary struct {
	Records   int64    `json:"records"`
	Good      int64    `json:"good"`
	Bad       int64    `json:"bad"`
	Uncertain int64    `json:"uncertain"`
	NoValue   int64    `json:"no_value"`
	Start     string   `json:"start"`
	End       string   `json:"end"`
	Nodes     []string `json:"nodes"`
}

type TrendPoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
}

type TrendSeries struct {
	Node   string       `json:"node"`
	Points []TrendPoint `json:"points"`
}

type AnomalyRow struct {
	Kind  string `json:"kind"`
	Node  string `json:"node"`
	Start string `json:"start"`
	End   string `json:"end"`
	Count int64  `json:"count"`
}

type AnomalyPage struct {
	Rows           []AnomalyRow `json:"rows"`
	Total          int64        `json:"total"`
	Bad            int64        `json:"bad"`
	Uncertain      int64        `json:"uncertain"`
	GoodEmpty      int64        `json:"good_empty"`
	AnomalyRecords int64        `json:"anomaly_records"`
}

type TagExpressionPreview struct {
	Count  int    `json:"count"`
	First  string `json:"first"`
	Second string `json:"second"`
	Last   string `json:"last"`
}

// ServerTag 服务器上浏览到的位号。
type ServerTag struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
}

// AppSettings 上一次会话的界面状态。与查询执行配置(QueryConfig)刻意分离:
// 表达式/CSV 模式只保存原始输入, 避免把展开后的上万位号灌回输入框。
type AppSettings struct {
	URL         string   `json:"url"`
	NS          uint16   `json:"ns"`
	Mode        string   `json:"mode"`   // direct | expression | csv
	Direct      string   `json:"direct"` // 直接输入的原文
	Expression  string   `json:"expression"`
	CSVName     string   `json:"csv_name"`
	CSVNodes    []string `json:"csv_nodes"`
	EndTime     string   `json:"end_time"`
	DurationSec int64    `json:"duration_sec"`
	PageSize    uint32   `json:"page_size"`
	Output      string   `json:"output"`
}

// SettingsStore 界面状态持久化。
type SettingsStore interface {
	Save(settings AppSettings) error
	Load() (AppSettings, bool, error)
}
