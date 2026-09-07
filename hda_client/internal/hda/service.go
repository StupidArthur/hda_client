package hda

import (
	"context"
	"fmt"
	"sync"
)

// ClientFactory 创建历史客户端(由 opcua adapter 提供)。
type ClientFactory func(ctx context.Context, url string) (HistoryClient, error)

// Service 业务编排: 连接管理 + 查询 + 浏览 + 配置。
type Service struct {
	factory ClientFactory
	mu      sync.RWMutex
	client  HistoryClient
	store   ConfigStore
}

func NewService(factory ClientFactory, store ConfigStore) *Service {
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

// Browse 浏览当前连接的服务器命名空间变量。
func (s *Service) Browse(ctx context.Context, ns uint16) ([]ServerTag, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.client == nil {
		return nil, fmt.Errorf("未连接服务器")
	}
	return s.client.BrowseVariables(ctx, ns)
}

func (s *Service) SaveConfig(cfg QueryConfig) error {
	if s.store == nil {
		return nil
	}
	cfg, err := cfg.NormalizeAndValidate()
	if err != nil {
		return err
	}
	return s.store.Save(cfg)
}

func (s *Service) LoadConfig() (QueryConfig, bool, error) {
	if s.store == nil {
		return QueryConfig{}, false, nil
	}
	cfg, found, err := s.store.Load()
	if err != nil || !found {
		return cfg, found, err
	}
	cfg, err = cfg.NormalizeAndValidate()
	return cfg, true, err
}
