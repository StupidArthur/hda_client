package opcua

import (
	"context"
	"fmt"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/id"
	"github.com/gopcua/opcua/ua"

	"hda_client/internal/hda"
)

// Client gopcua 实现的 HistoryClient。
type Client struct {
	c *opcua.Client
}

// NewClient 建立到服务器的连接(免安全模式)。
func NewClient(ctx context.Context, url string) (*Client, error) {
	c, err := opcua.NewClient(url,
		opcua.SecurityMode(ua.MessageSecurityModeNone),
		opcua.DialTimeout(10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("创建客户端失败: %w", err)
	}
	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := c.Connect(connectCtx); err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}
	return &Client{c: c}, nil
}

func (c *Client) Close() error {
	if c.c == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.c.Close(ctx)
}

// ReadRaw 读取单个位号的完整时间范围，并使用 continuation point 串行翻页。
func (c *Client) ReadRaw(ctx context.Context, nodeID string, start, end time.Time) ([]hda.DataPoint, error) {
	return c.ReadRawWithProgress(ctx, nodeID, start, end, 5000, nil)
}

// ReadRawWithProgress keeps HistoryRead continuation points serial for this
// node and reports each successfully decoded server page immediately.
func (c *Client) ReadRawWithProgress(ctx context.Context, nodeID string, start, end time.Time, pageSize uint32, onPage func(records int)) ([]hda.DataPoint, error) {
	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return nil, fmt.Errorf("无效 NodeID %q: %w", nodeID, err)
	}

	points := make([]hda.DataPoint, 0)
	node := &ua.HistoryReadValueID{NodeID: id, DataEncoding: &ua.QualifiedName{}}
	details := &ua.ReadRawModifiedDetails{
		IsReadModified:   false,
		StartTime:        start,
		EndTime:          end,
		NumValuesPerNode: pageSize,
		ReturnBounds:     false,
	}
	var previousContinuationPoint []byte
	stalledPages := 0
	completed := false
	defer func() {
		if !completed && len(node.ContinuationPoint) > 0 {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = c.releaseHistoryContinuationPoint(releaseCtx, node, details)
		}
	}()

	for {
		resp, err := c.c.HistoryReadRawModified(ctx, []*ua.HistoryReadValueID{node}, details)
		if err != nil {
			return nil, err
		}
		if resp == nil || len(resp.Results) != 1 {
			return nil, fmt.Errorf("位号 %q 的历史读取返回了无效结果", nodeID)
		}

		r := resp.Results[0]
		if statusSeverity(r.StatusCode) == "Bad" {
			return nil, fmt.Errorf("位号 %q 的历史读取失败: %s", nodeID, r.StatusCode)
		}
		pageRecords := 0
		if r.HistoryData != nil {
			hd, ok := r.HistoryData.Value.(*ua.HistoryData)
			if !ok && r.HistoryData.Value != nil {
				return nil, fmt.Errorf("位号 %q 的历史数据类型无效", nodeID)
			}
			if hd != nil {
				for _, dv := range hd.DataValues {
					var rawValue interface{}
					if dv.Value != nil {
						rawValue = dv.Value.Value()
					}
					p := hda.DataPoint{
						Time:     dv.SourceTimestamp.UTC(),
						Quality:  qualityName(dv.Status),
						HasValue: rawValue != nil,
					}
					if rawValue != nil {
						value, err := toFloat(rawValue)
						if err != nil {
							return nil, fmt.Errorf("位号 %q 在 %s 的值无法转为数值: %w", nodeID, p.Time.Format(time.RFC3339Nano), err)
						}
						p.Value = value
					}
					points = append(points, p)
					pageRecords++
				}
				if pageRecords > 0 && onPage != nil {
					onPage(pageRecords)
				}
			}
		}

		if len(r.ContinuationPoint) == 0 {
			completed = true
			break
		}
		if string(r.ContinuationPoint) == string(previousContinuationPoint) && pageRecords == 0 {
			stalledPages++
			if stalledPages >= 2 {
				return nil, fmt.Errorf("位号 %q 的历史读取连续返回空页，continuation point 未推进", nodeID)
			}
		} else {
			stalledPages = 0
		}
		previousContinuationPoint = append(previousContinuationPoint[:0], r.ContinuationPoint...)
		node.ContinuationPoint = append(node.ContinuationPoint[:0], r.ContinuationPoint...)
	}
	return points, nil
}

func (c *Client) releaseHistoryContinuationPoint(ctx context.Context, node *ua.HistoryReadValueID, details *ua.ReadRawModifiedDetails) error {
	req := &ua.HistoryReadRequest{
		HistoryReadDetails: &ua.ExtensionObject{
			TypeID:       ua.NewFourByteExpandedNodeID(0, id.ReadRawModifiedDetails_Encoding_DefaultBinary),
			EncodingMask: ua.ExtensionObjectBinary,
			Value:        details,
		},
		TimestampsToReturn:        ua.TimestampsToReturnBoth,
		ReleaseContinuationPoints: true,
		NodesToRead:               []*ua.HistoryReadValueID{node},
	}
	return c.c.Send(ctx, req, func(ua.Response) error { return nil })
}

func qualityName(s ua.StatusCode) string {
	if s == ua.StatusOK {
		return "Good"
	}
	return fmt.Sprintf("%s/0x%08X", statusSeverity(s), uint32(s))
}

func statusSeverity(s ua.StatusCode) string {
	switch uint32(s) & 0xC0000000 {
	case 0:
		return "Good"
	case 0x40000000:
		return "Uncertain"
	default:
		return "Bad"
	}
}

func toFloat(v interface{}) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case float32:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case int32:
		return float64(t), nil
	case int16:
		return float64(t), nil
	case int8:
		return float64(t), nil
	case int:
		return float64(t), nil
	case uint64:
		return float64(t), nil
	case uint32:
		return float64(t), nil
	case uint16:
		return float64(t), nil
	case uint8:
		return float64(t), nil
	case uint:
		return float64(t), nil
	case bool:
		if t {
			return 1, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("不支持的类型 %T", v)
	}
}

// BrowseVariables 从 Objects 递归浏览, 收集指定命名空间的变量位号。
func (c *Client) BrowseVariables(ctx context.Context, ns uint16) ([]hda.ServerTag, error) {
	objects := c.c.Node(ua.NewNumericNodeID(0, 85))
	out := []hda.ServerTag{}
	seenTags := map[string]bool{}
	visited := map[string]bool{}

	var walk func(n *opcua.Node, depth int) error
	walk = func(n *opcua.Node, depth int) error {
		if depth > 12 {
			return nil
		}
		key := n.ID.String()
		if visited[key] {
			return nil
		}
		visited[key] = true
		children, err := n.Children(ctx, 0, ua.NodeClassUnspecified)
		if err != nil {
			return fmt.Errorf("浏览节点 %q 失败: %w", key, err)
		}
		for _, ch := range children {
			cls, err := ch.NodeClass(ctx)
			if err != nil {
				return fmt.Errorf("读取节点 %q 类型失败: %w", ch.ID, err)
			}
			if cls == ua.NodeClassVariable && ch.ID.Namespace() == ns {
				bn, err := ch.BrowseName(ctx)
				if err != nil {
					return fmt.Errorf("读取节点 %q 名称失败: %w", ch.ID, err)
				}
				childKey := ch.ID.String()
				if !seenTags[childKey] {
					seenTags[childKey] = true
					out = append(out, hda.ServerTag{NodeID: childKey, Name: bn.Name})
				}
			}
			if cls == ua.NodeClassObject || cls == ua.NodeClassVariable {
				if err := walk(ch, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := walk(objects, 0); err != nil {
		return nil, err
	}
	return out, nil
}
