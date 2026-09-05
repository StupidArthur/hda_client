package bindings

import (
	"hda_client/internal/hda"
)

// SettingsBinding 查询配置的保存/读取。
type SettingsBinding struct {
	service *hda.Service
}

func NewSettingsBinding(service *hda.Service) *SettingsBinding {
	return &SettingsBinding{service: service}
}

func (b *SettingsBinding) SaveConfig(cfg hda.QueryConfig) error {
	return b.service.SaveConfig(cfg)
}

// LoadConfig 返回 (cfg, found)。
func (b *SettingsBinding) LoadConfig() (hda.QueryConfig, bool, error) {
	return b.service.LoadConfig()
}

