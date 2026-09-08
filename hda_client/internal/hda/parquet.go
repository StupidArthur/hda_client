package hda

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
)

type ParquetWriter struct {
	path, tmp string
	file      *os.File
	writer    *parquet.GenericWriter[ParquetRow]
	total     int64
	finished  bool
}

func NewParquetWriter(path string) (*ParquetWriter, error) {
	if path == "" {
		return nil, fmt.Errorf("输出文件不能为空")
	}
	if filepath.Ext(path) == "" {
		path += ".parquet"
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建输出目录: %w", err)
	}
	f, err := os.CreateTemp(dir, ".hda-*.partial")
	if err != nil {
		return nil, fmt.Errorf("创建 parquet 文件: %w", err)
	}
	return &ParquetWriter{path: path, tmp: f.Name(), file: f, writer: parquet.NewGenericWriter[ParquetRow](f)}, nil
}

func (w *ParquetWriter) Write(ctx context.Context, result TagResult) error {
	const chunkSize = 4096
	for start := 0; start < len(result.Points); start += chunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(start+chunkSize, len(result.Points))
		rows := make([]ParquetRow, end-start)
		for i, point := range result.Points[start:end] {
			rows[i] = ParquetRow{Timestamp: point.Time.UTC().UnixNano(), Node: result.Tag, Value: point.Value, Quality: point.Quality, HasValue: point.HasValue}
		}
		if _, err := w.writer.Write(rows); err != nil {
			return fmt.Errorf("写入 parquet: %w", err)
		}
		w.total += int64(len(rows))
	}
	return nil
}

func (w *ParquetWriter) Close() (int64, error) {
	if w.finished {
		return w.total, nil
	}
	if err := w.writer.Close(); err != nil {
		w.Abort()
		return 0, fmt.Errorf("完成 parquet: %w", err)
	}
	if err := w.file.Close(); err != nil {
		w.Abort()
		return 0, fmt.Errorf("关闭 parquet: %w", err)
	}
	if err := os.Rename(w.tmp, w.path); err != nil {
		w.Abort()
		return 0, fmt.Errorf("保存 parquet: %w", err)
	}
	w.finished = true
	return w.total, nil
}

func (w *ParquetWriter) Abort() {
	if w == nil || w.finished {
		return
	}
	w.finished = true
	if w.writer != nil {
		_ = w.writer.Close()
	}
	if w.file != nil {
		_ = w.file.Close()
	}
	if w.tmp != "" {
		_ = os.Remove(w.tmp)
	}
}

func WriteParquet(path string, results []TagResult) (int64, error) {
	w, err := NewParquetWriter(path)
	if err != nil {
		return 0, err
	}
	defer w.Abort()
	for _, result := range results {
		if err := w.Write(context.Background(), result); err != nil {
			return 0, err
		}
	}
	return w.Close()
}

func ReadParquetPage(path, node string, offset, limit int) (ParquetPage, error) {
	var selected []string
	if node != "" {
		selected = []string{node}
	}
	return ReadParquetNodesPage(path, selected, offset, limit)
}

func ReadParquetNodesPage(path string, selected []string, offset, limit int) (ParquetPage, error) {
	if limit < 1 || limit > 2000 {
		return ParquetPage{}, fmt.Errorf("页大小必须在 1-2000 之间")
	}
	if offset < 0 {
		return ParquetPage{}, fmt.Errorf("页码不能小于 0")
	}
	f, err := os.Open(path)
	if err != nil {
		return ParquetPage{}, fmt.Errorf("打开 parquet: %w", err)
	}
	defer f.Close()
	r := parquet.NewGenericReader[ParquetRow](f)
	defer r.Close()
	page := ParquetPage{Rows: make([]ParquetViewRow, 0, limit)}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		selectedSet[name] = struct{}{}
	}
	nodes := make(map[string]struct{})
	buf := make([]ParquetRow, 512)
	matched := 0
	for {
		n, readErr := r.Read(buf)
		for _, row := range buf[:n] {
			nodes[row.Node] = struct{}{}
			if len(selectedSet) > 0 {
				if _, ok := selectedSet[row.Node]; !ok {
					continue
				}
			}
			if matched >= offset && len(page.Rows) < limit {
				page.Rows = append(page.Rows, ParquetViewRow{Timestamp: time.Unix(0, row.Timestamp).UTC().Format(time.RFC3339Nano), Node: row.Node, Value: row.Value, Quality: row.Quality, HasValue: row.HasValue})
			}
			matched++
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return ParquetPage{}, fmt.Errorf("读取 parquet: %w", readErr)
		}
	}
	page.Total = int64(matched)
	page.Nodes = make([]string, 0, len(nodes))
	for name := range nodes {
		page.Nodes = append(page.Nodes, name)
	}
	sort.Strings(page.Nodes)
	return page, nil
}

func ReadParquetSummary(path string) (ParquetSummary, error) {
	return readParquetSummary(path, nil)
}

func ReadParquetSelectedSummary(path string, selected []string) (ParquetSummary, error) {
	return readParquetSummary(path, selected)
}

func readParquetSummary(path string, selected []string) (ParquetSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return ParquetSummary{}, fmt.Errorf("打开 parquet: %w", err)
	}
	defer f.Close()
	r := parquet.NewGenericReader[ParquetRow](f)
	defer r.Close()
	var out ParquetSummary
	nodes := make(map[string]struct{})
	selectedSet := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		selectedSet[name] = struct{}{}
	}
	buf := make([]ParquetRow, 2048)
	var first, last int64
	for {
		n, readErr := r.Read(buf)
		for _, row := range buf[:n] {
			if len(selectedSet) > 0 {
				if _, ok := selectedSet[row.Node]; !ok {
					continue
				}
			}
			out.Records++
			nodes[row.Node] = struct{}{}
			if first == 0 || row.Timestamp < first {
				first = row.Timestamp
			}
			if row.Timestamp > last {
				last = row.Timestamp
			}
			if !row.HasValue {
				out.NoValue++
			}
			switch {
			case strings.HasPrefix(row.Quality, "Good"):
				out.Good++
			case strings.HasPrefix(row.Quality, "Uncertain"):
				out.Uncertain++
			default:
				out.Bad++
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return ParquetSummary{}, fmt.Errorf("读取 parquet: %w", readErr)
		}
	}
	for name := range nodes {
		out.Nodes = append(out.Nodes, name)
	}
	sort.Strings(out.Nodes)
	if first != 0 {
		out.Start = time.Unix(0, first).UTC().Format(time.RFC3339Nano)
	}
	if last != 0 {
		out.End = time.Unix(0, last).UTC().Format(time.RFC3339Nano)
	}
	return out, nil
}

func ReadParquetTrend(path string, selected []string, maxPoints int) ([]TrendSeries, error) {
	if len(selected) == 0 {
		return []TrendSeries{}, nil
	}
	if maxPoints < 50 {
		maxPoints = 50
	}
	if maxPoints > 2000 {
		maxPoints = 2000
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开 parquet: %w", err)
	}
	defer f.Close()
	r := parquet.NewGenericReader[ParquetRow](f)
	defer r.Close()
	wanted := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		wanted[name] = struct{}{}
	}
	buf := make([]ParquetRow, 2048)
	var minTime, maxTime int64
	for {
		n, readErr := r.Read(buf)
		for _, row := range buf[:n] {
			if _, ok := wanted[row.Node]; ok && row.HasValue {
				if minTime == 0 || row.Timestamp < minTime {
					minTime = row.Timestamp
				}
				if row.Timestamp > maxTime {
					maxTime = row.Timestamp
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("读取 parquet: %w", readErr)
		}
	}
	if minTime == 0 {
		return []TrendSeries{}, nil
	}
	_ = r.Close()
	_ = f.Close()
	f, err = os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开 parquet: %w", err)
	}
	defer f.Close()
	r = parquet.NewGenericReader[ParquetRow](f)
	defer r.Close()
	type bucket struct {
		count         int64
		sum, min, max float64
		timestamp     int64
	}
	buckets := make(map[string]map[int]*bucket, len(selected))
	span := maxTime - minTime + 1
	bucketWidth := max(int64(1), (span+int64(maxPoints)-1)/int64(maxPoints))
	for {
		n, readErr := r.Read(buf)
		for _, row := range buf[:n] {
			if _, ok := wanted[row.Node]; !ok || !row.HasValue {
				continue
			}
			index := int((row.Timestamp - minTime) / bucketWidth)
			byNode := buckets[row.Node]
			if byNode == nil {
				byNode = make(map[int]*bucket)
				buckets[row.Node] = byNode
			}
			b := byNode[index]
			if b == nil {
				b = &bucket{min: row.Value, max: row.Value, timestamp: minTime + int64(index)*bucketWidth + bucketWidth/2}
				byNode[index] = b
			}
			b.count++
			b.sum += row.Value
			if row.Value < b.min {
				b.min = row.Value
			}
			if row.Value > b.max {
				b.max = row.Value
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("读取 parquet: %w", readErr)
		}
	}
	out := make([]TrendSeries, 0, len(selected))
	for _, name := range selected {
		series := TrendSeries{Node: name, Points: []TrendPoint{}}
		indexes := make([]int, 0, len(buckets[name]))
		for index := range buckets[name] {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		for _, index := range indexes {
			b := buckets[name][index]
			series.Points = append(series.Points, TrendPoint{Timestamp: time.Unix(0, b.timestamp).UTC().Format(time.RFC3339Nano), Value: b.sum / float64(b.count), Min: b.min, Max: b.max})
		}
		out = append(out, series)
	}
	return out, nil
}
