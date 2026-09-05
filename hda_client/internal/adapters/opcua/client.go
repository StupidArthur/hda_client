package opcua

import (
	"context"
	"fmt"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"

	"hda_client/internal/hda"
)

// Client gopcua 实现的 HistoryClient。
type Client struct {
	c *opcua.Client
}

// NewClient 建立到服务器的连接(免安全模式)。
func NewClient(ctx context.Context, url string) (*Client, error) {
	c, err := opcua.NewClient(url, opcua.SecurityMode(ua.MessageSecurityModeNone))
	if err != nil {
		return nil, fmt.Errorf("创建客户端失败: %w", err)
	}
	if err := c.Connect(ctx); err != nil {
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

// ReadRaw 读单个位号单个时间段。分段由调用方保证(<=15min, <=900点)。
func (c *Client) ReadRaw(ctx context.Context, nodeID string, start, end time.Time) ([]hda.DataPoint, error) {
	id, err := ua.ParseNodeID(nodeID)
	if err != nil {
		return nil, fmt.Errorf("无效 NodeID %q: %w", nodeID, err)
	}
	resp, err := c.c.HistoryReadRawModified(ctx,
		[]*ua.HistoryReadValueID{{NodeID: id, DataEncoding: &ua.QualifiedName{}}},
		&ua.ReadRawModifiedDetails{
			IsReadModified:   false,
			StartTime:        start,
			EndTime:          end,
			NumValuesPerNode: 5000,
			ReturnBounds:     false,
		})
	if err != nil {
		return nil, err
	}
	points := make([]hda.DataPoint, 0)
	for _, r := range resp.Results {
		if r.StatusCode != ua.StatusOK {
			continue
		}
		hd, ok := r.HistoryData.Value.(*ua.HistoryData)
		if !ok || hd == nil {
			continue
		}
		for _, dv := range hd.DataValues {
			p := hda.DataPoint{
				Time:    dv.SourceTimestamp.UTC(),
				Quality: qualityName(dv.Status),
				HasValue: dv.Value != nil,
			}
			if dv.Value != nil {
				p.Value = toFloat(dv.Value.Value())
			}
			points = append(points, p)
		}
	}
	return points, nil
}

func qualityName(s ua.StatusCode) string {
	if s == ua.StatusOK {
		return "Good"
	}
	return fmt.Sprintf("Bad/0x%08X", uint32(s))
}

func toFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int64:
		return float64(t)
	case int32:
		return float64(t)
	case int:
		return float64(t)
	case uint64:
		return float64(t)
	case uint32:
		return float64(t)
	case bool:
		if t {
			return 1
		}
		return 0
	default:
		return 0
	}
}

// BrowseVariables 从 Objects 递归浏览, 收集指定命名空间的变量位号。
func (c *Client) BrowseVariables(ctx context.Context, ns uint16) ([]hda.ServerTag, error) {
	objects := c.c.Node(ua.NewNumericNodeID(0, 85))
	out := []hda.ServerTag{}
	seen := map[string]bool{}

	var walk func(n *opcua.Node, depth int) error
	walk = func(n *opcua.Node, depth int) error {
		if depth > 12 {
			return nil
		}
		children, err := n.Children(ctx, 0, ua.NodeClassUnspecified)
		if err != nil {
			return nil
		}
		for _, ch := range children {
			if ch.ID.Namespace() != ns {
				continue
			}
			cls, err := ch.NodeClass(ctx)
			if err != nil {
				continue
			}
			if cls == ua.NodeClassVariable {
				bn, _ := ch.BrowseName(ctx)
				key := ch.ID.String()
				if !seen[key] {
					seen[key] = true
					out = append(out, hda.ServerTag{NodeID: key, Name: bn.Name})
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

