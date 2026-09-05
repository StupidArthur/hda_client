package hda

import (
	"context"
	"fmt"
)

// ClientFactory 创建历史客户端(由 opcua adapter 提供)。
type ClientFactory func(url string) (HistoryClient, error)

// Service 业务编排: 连接管理 + 查询 + 浏览 + 配置。
type Service struct {
	factory ClientFactory
	client  HistoryClient
	store   ConfigStore
}

func NewService(factory ClientFactory, store ConfigStore) *Service {
	return &Service{factory: factory, store: store}
}

// Connect 建立到服务器的连接。
func (s *Service) Connect(ctx context.Context, url string) error {
	if s.client != nil {
		s.client.Close()
		s.client = nil
	}
	c, err := s.factory(url)
	if err != nil {
		return err
	}
	s.client = c
	return nil
}

func (s *Service) Disconnect() {
	if s.client != nil {
		s.client.Close()
		s.client = nil
	}
}

// IsConnected 是否已连接。
func (s *Service) IsConnected() bool {
	return s.client != nil
}

// RunQuery 执行查询, onProgress 为进度回调。
func (s *Service) RunQuery(ctx context.Context, cfg QueryConfig, onProgress func(QueryProgress)) ([]TagResult, error) {
	if s.client == nil {
		return nil, fmt.Errorf("未连接服务器")
	}
	return QueryRunner(ctx, s.client, cfg, onProgress)
}

// Browse 浏览当前连接的服务器命名空间变量。
func (s *Service) Browse(ctx context.Context, ns uint16) ([]ServerTag, error) {
	if s.client == nil {
		return nil, fmt.Errorf("未连接服务器")
	}
	return s.client.BrowseVariables(ctx, ns)
}

func (s *Service) SaveConfig(cfg QueryConfig) error {
	if s.store == nil {
		return nil
	}
	return s.store.Save(cfg)
}

func (s *Service) LoadConfig() (QueryConfig, bool, error) {
	if s.store == nil {
		return QueryConfig{}, false, nil
	}
	return s.store.Load()
}
