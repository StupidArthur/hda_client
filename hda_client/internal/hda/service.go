package hda

import (
	"context"
	"fmt"
	"sync"
)

// ClientFactory 创建历史客户端(由 opcua adapter 提供)。
type ClientFactory func(ctx context.Context, url string) (HistoryClient, error)

// Service 业务编排: 连接管理 + 查询 + 浏览 + 界面状态。
type Service struct {
	factory ClientFactory
	mu      sync.RWMutex
	client  HistoryClient
	store   SettingsStore
}

func NewService(factory ClientFactory, store SettingsStore) *Service {
	return &Service{factory: factory, store: store}
}

// Connect 建立到服务器的连接。
func (s *Service) Connect(ctx context.Context, url string) error {
	if url == "" {
		return fmt.Errorf("URL 不能为空")
	}
	c, err := s.factory(ctx, url)
	if err != nil {
		return err
	}
	s.mu.Lock()
	old := s.client
	s.client = c
	s.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

func (s *Service) Disconnect() {
	s.mu.Lock()
	client := s.client
	s.client = nil
	s.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
}

// IsConnected 是否已连接。
func (s *Service) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client != nil
}

// RunQuery 执行查询, onProgress 为进度回调。
func (s *Service) RunQuery(ctx context.Context, cfg QueryConfig, onProgress func(QueryProgress)) ([]TagResult, error) {
	cfg, err := cfg.NormalizeAndValidate()
	if err != nil {
		return nil, err
	}
	client, err := s.factory(ctx, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("连接服务器失败: %w", err)
	}
	defer client.Close()
	return QueryRunner(ctx, client, cfg, onProgress)
}

func (s *Service) RunQueryEach(ctx context.Context, cfg QueryConfig, onProgress func(QueryProgress), consume func(TagResult) error) (int, int64, error) {
	cfg, err := cfg.NormalizeAndValidate()
	if err != nil {
		return 0, 0, err
	}
	client, err := s.factory(ctx, cfg.URL)
	if err != nil {
		return 0, 0, fmt.Errorf("连接服务器失败: %w", err)
	}
	defer client.Close()
	return QueryRunnerEach(ctx, client, cfg, onProgress, consume)
}

// Browse 浏览当前连接的服务器命名空间变量。
func (s *Service) Browse(ctx context.Context, ns uint16) ([]ServerTag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.client == nil {
		return nil, fmt.Errorf("未连接服务器")
	}
	return s.client.BrowseVariables(ctx, ns)
}

// SaveSettings 持久化界面状态。界面输入允许是半成品, 不做查询级强校验,
// 只归一化 mode 默认值, 保证下次启动能还原到同样的输入现场。
func (s *Service) SaveSettings(settings AppSettings) error {
	if s.store == nil {
		return nil
	}
	if settings.Mode == "" {
		settings.Mode = "direct"
	}
	return s.store.Save(settings)
}

// LoadSettings 读取上次保存的界面状态。未保存过时返回零值(以 URL 为空
// 判断), 让前端保持出厂默认, 而不是抛错打断启动。
func (s *Service) LoadSettings() (AppSettings, error) {
	if s.store == nil {
		return AppSettings{}, nil
	}
	settings, found, err := s.store.Load()
	if err != nil || !found {
		return AppSettings{}, err
	}
	if settings.Mode == "" {
		settings.Mode = "direct"
	}
	return settings, nil
}
