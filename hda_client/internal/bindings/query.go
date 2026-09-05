package bindings

import (
	"context"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"hda_client/internal/hda"
)

// QueryBinding 查询发起/取消/进度事件。
type QueryBinding struct {
	ctx     context.Context
	service *hda.Service
	cancel  context.CancelFunc
	mu      sync.Mutex
	active  bool
}

func NewQueryBinding(service *hda.Service) *QueryBinding {
	return &QueryBinding{service: service}
}

// Startup 注入 runtime context, 由 app 启动时调用。
func (b *QueryBinding) Startup(ctx context.Context) {
	b.ctx = ctx
}

// StartQuery 后台启动一次查询, 进度经事件 hda:progress 推送, 完成推 hda:done, 失败推 hda:error。
func (b *QueryBinding) StartQuery(cfg hda.QueryConfig) (string, error) {
	b.mu.Lock()
	if b.active {
		b.mu.Unlock()
		return "", hda.ErrBusy{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	b.active = true
	b.mu.Unlock()

	go func() {
		defer func() {
			b.mu.Lock()
			b.active = false
			b.cancel = nil
			b.mu.Unlock()
		}()
		results, err := b.service.RunQuery(ctx, cfg, func(p hda.QueryProgress) {
			if b.ctx != nil {
				runtime.EventsEmit(b.ctx, "hda:progress", p)
			}
		})
		if b.ctx == nil {
			return
		}
		if err != nil {
			if ctx.Err() != nil {
				runtime.EventsEmit(b.ctx, "hda:done", map[string]interface{}{"canceled": true})
			} else {
				runtime.EventsEmit(b.ctx, "hda:error", err.Error())
			}
			return
		}
		runtime.EventsEmit(b.ctx, "hda:done", results)
	}()

	return "running", nil
}

// CancelQuery 取消当前查询。
func (b *QueryBinding) CancelQuery() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
	}
}

