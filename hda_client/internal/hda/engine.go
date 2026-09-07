package hda

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// QueryRunner 执行一次完整查询, 返回按位号分组的结果。
// 不同位号可并发；单位号的 continuation point 分页由 HistoryClient 串行完成。
// done 回调用于进度通知, 可传 nil。
func QueryRunner(
	ctx context.Context,
	client HistoryClient,
	cfg QueryConfig,
	done func(QueryProgress),
) ([]TagResult, error) {
	var err error
	cfg, err = cfg.NormalizeAndValidate()
	if err != nil {
		return nil, err
	}
	end, err := time.ParseInLocation(queryTimeLayout, cfg.EndTime, time.Local)
	if err != nil {
		return nil, fmt.Errorf("结束时间格式错误(需 yyyy-MM-ddTHH:mm:ss): %w", err)
	}
	start := end.Add(-time.Duration(cfg.DurationSec) * time.Second)

	type job struct {
		tag    string
		nodeID string
	}
	jobs := make([]job, 0, len(cfg.Tags))
	for _, tag := range cfg.Tags {
		jobs = append(jobs, job{tag: tag, nodeID: BuildNodeID(cfg.NS, tag)})
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
	jobCh := make(chan job)
	ch := make(chan item)

	// 进度计数(线程安全用原子或串行提交)
	var doneCount int
	var records int64

	emitProgress := func(active bool) {
		if done != nil {
			done(QueryProgress{Done: doneCount, Total: total, Records: records, Active: active})
		}
	}
	emitProgress(true)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workerCount := min(cfg.Concurrency, total)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for j := range jobCh {
				data, err := client.ReadRaw(ctx, j.nodeID, start, end)
				ch <- item{j.tag, data, err}
			}
		}()
	}
	go func() {
		defer close(jobCh)
		for _, j := range jobs {
			select {
			case jobCh <- j:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(ch)
	}()

	results := map[string]*TagResult{}
	var firstErr error
	for it := range ch {
		doneCount++
		if it.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("查询位号 %q 失败: %w", it.tag, it.err)
				cancel()
			}
			continue
		}
		if firstErr != nil {
			continue
		}
		records += int64(len(it.data))
		r := results[it.tag]
		if r == nil {
			r = &TagResult{Tag: it.tag}
			results[it.tag] = r
		}
		r.Points = append(r.Points, it.data...)
		emitProgress(true)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	emitProgress(false)

	ordered := make([]TagResult, 0, len(results))
	for _, tag := range cfg.Tags {
		if r, ok := results[tag]; ok {
			sort.SliceStable(r.Points, func(i, j int) bool {
				return r.Points[i].Time.Before(r.Points[j].Time)
			})
			ordered = append(ordered, *r)
		}
	}
	return ordered, nil
}

// BuildNodeID 构造 ns;id 形式的 NodeID 字符串。
func BuildNodeID(ns uint16, id string) string {
	return fmt.Sprintf("ns=%d;s=%s", ns, strings.TrimSpace(id))
}
