package hda

import (
	"time"
)

// QueryConfig 一次查询的完整配置。
type QueryConfig struct {
	URL         string   `json:"url"`
	NS          uint16   `json:"ns"`
	Tags        []string `json:"tags"`
	EndTime     string   `json:"end_time"` // RFC3339 本地时间, 如 2026-09-04T10:00:00
	DurationSec int64    `json:"duration_sec"`
	Concurrency int      `json:"concurrency"`
}

// DataPoint 一条历史数据。
type DataPoint struct {
	Time    time.Time `json:"time"`
	Value   float64   `json:"value"`
	Quality string    `json:"quality"`
	HasValue bool     `json:"has_value"`
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

// ServerTag 服务器上浏览到的位号。
type ServerTag struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
}

// ConfigStore 查询配置持久化。
type ConfigStore interface {
	Save(cfg QueryConfig) error
	Load() (QueryConfig, bool, error)
}
