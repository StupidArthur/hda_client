package hda

import (
	"context"
	"time"
)

// HistoryClient 历史数据访问能力(由 opcua adapter 实现)。
type HistoryClient interface {
	// ReadRaw 读取单个位号单个时间段的原始历史。
	ReadRaw(ctx context.Context, nodeID string, start, end time.Time) ([]DataPoint, error)
	// BrowseVariables 浏览服务器上指定命名空间的变量位号。
	BrowseVariables(ctx context.Context, ns uint16) ([]ServerTag, error)
	// Close 关闭连接。
	Close() error
}

// ProgressiveHistoryClient is optional so existing adapters and callers keep
// working. Implementations report records after every server page while still
// reading continuation points serially for one node.
type ProgressiveHistoryClient interface {
	HistoryClient
	ReadRawWithProgress(ctx context.Context, nodeID string, start, end time.Time, onPage func(records int)) ([]DataPoint, error)
}
