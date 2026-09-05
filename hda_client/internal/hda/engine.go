package hda

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// 分段策略：单位号、每段最大 15 分钟(秒级约 900 点, 留余量防服务器截断)。
const SegmentMinutes = 15

// QueryRunner 执行一次完整查询, 返回按位号分组的结果。
// done 回调用于进度通知, 可传 nil。
func QueryRunner(
	ctx context.Context,
	client HistoryClient,
	cfg QueryConfig,
	done func(QueryProgress),
) ([]TagResult, error) {
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 16
	}
	seg := time.Duration(SegmentMinutes) * time.Minute
	end, err := time.ParseInLocation("2006-01-02T15:04:05", cfg.EndTime, time.Local)
	if err != nil {
		return nil, fmt.Errorf("结束时间格式错误(需 yyyy-MM-ddTHH:mm:ss): %w", err)
	}
	start := end.Add(-time.Duration(cfg.DurationSec) * time.Second)

	type job struct {
		tag    string
		nodeID string
		s      time.Time
		e      time.Time
	}
	var jobs []job
	for _, tag := range cfg.Tags {
		nodeID := BuildNodeID(cfg.NS, tag)
		for t := start; t.Before(end); t = t.Add(seg) {
			e := t.Add(seg)
			if e.After(end) {
				e = end
			}
			jobs = append(jobs, job{tag, nodeID, t, e})
		}
	}
	total := len(jobs)
	if total == 0 {
		return nil, fmt.Errorf("没有可查询的分段(检查 tags 与时长)")
	}

	// 结果按 tag 聚合; 用 channel 收集 worker 产出。
	type item struct {
		tag  string
		data []DataPoint
		err  error
	}
	ch := make(chan item, total)
	sem := make(chan struct{}, cfg.Concurrency)

	// 进度计数(线程安全用原子或串行提交)
	var doneCount int
	var records int64

	emitProgress := func(active bool) {
		if done != nil {
			done(QueryProgress{Done: doneCount, Total: total, Records: records, Active: active})
		}
	}
	emitProgress(true)

	var pending int
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, j := range jobs {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		sem <- struct{}{}
		pending++
		go func(j job) {
			defer func() { <-sem }()
			data, err := client.ReadRaw(ctx, j.nodeID, j.s, j.e)
			ch <- item{j.tag, data, err}
		}(j)
	}

	results := map[string]*TagResult{}
	for i := 0; i < pending; i++ {
		it := <-ch
		doneCount++
		if it.err != nil {
			// 单段失败不中断整体; 记录空结果
			records += 0
		} else {
			records += int64(len(it.data))
			r := results[it.tag]
			if r == nil {
				r = &TagResult{Tag: it.tag}
				results[it.tag] = r
			}
			r.Points = append(r.Points, it.data...)
		}
		emitProgress(true)
	}
	emitProgress(false)

	ordered := make([]TagResult, 0, len(results))
	for _, tag := range cfg.Tags {
		if r, ok := results[tag]; ok {
			ordered = append(ordered, *r)
		}
	}
	return ordered, nil
}

// BuildNodeID 构造 ns;id 形式的 NodeID 字符串。
func BuildNodeID(ns uint16, id string) string {
	return fmt.Sprintf("ns=%d;s=%s", ns, strings.TrimSpace(id))
}
