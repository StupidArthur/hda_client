package bindings

import (
	"context"

	"hda_client/internal/hda"
)

// ConnectionBinding 连接与位号浏览。
type ConnectionBinding struct {
	ctx     context.Context
	service *hda.Service
}

func NewConnectionBinding(service *hda.Service) *ConnectionBinding {
	return &ConnectionBinding{service: service}
}

// Startup 注入 runtime context。
func (b *ConnectionBinding) Startup(ctx context.Context) {
	b.ctx = ctx
}

// Connect 建立连接并保持。
func (b *ConnectionBinding) Connect(url string) error {
	return b.service.Connect(b.ctx, url)
}

func (b *ConnectionBinding) Disconnect() {
	b.service.Disconnect()
}

func (b *ConnectionBinding) IsConnected() bool {
	return b.service.IsConnected()
}

// BrowseTags 浏览已连接服务器的命名空间变量。
func (b *ConnectionBinding) BrowseTags(ns uint16) ([]hda.ServerTag, error) {
	return b.service.Browse(b.ctx, ns)
}

