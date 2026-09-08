package app

import (
	"context"
	"os"
	"path/filepath"

	"hda_client/internal/adapters/opcua"
	"hda_client/internal/adapters/storage"
	"hda_client/internal/bindings"
	"hda_client/internal/hda"
)

// Container 组合根: 创建所有具体依赖并连接。
type Container struct {
	Connection *bindings.ConnectionBinding
	Query      *bindings.QueryBinding
	Settings   *bindings.SettingsBinding
	svc        *hda.Service
}

func NewContainer() *Container {
	configDir, _ := os.UserConfigDir()
	store := storage.NewJSONStore(filepath.Join(configDir, "hda_client", "config.json"))

	factory := func(ctx context.Context, url string) (hda.HistoryClient, error) {
		return opcua.NewClient(ctx, url)
	}

	svc := hda.NewService(factory, store)

	return &Container{
		Connection: bindings.NewConnectionBinding(svc),
		Query:      bindings.NewQueryBinding(svc),
		Settings:   bindings.NewSettingsBinding(svc),
		svc:        svc,
	}
}

// Startup 注入 runtime context。
func (c *Container) Startup(ctx context.Context) {
	c.Connection.Startup(ctx)
	c.Query.Startup(ctx)
}

// Shutdown 关闭连接。
func (c *Container) Shutdown(_ context.Context) {
	c.svc.Disconnect()
}
