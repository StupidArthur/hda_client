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
