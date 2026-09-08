package bindings

import (
	"hda_client/internal/hda"
)

// SettingsBinding 界面状态持久化。
// 注意: Wails 绑定方法最多支持 (T, error) 两个返回值, 多余的返回值
// 会被运行时静默丢弃, 因此未保存过的情况以零值 AppSettings 表达,
// 前端用 URL 为空判断。
type SettingsBinding struct {
	service *hda.Service
}

func NewSettingsBinding(service *hda.Service) *SettingsBinding {
	return &SettingsBinding{service: service}
}

func (b *SettingsBinding) SaveSettings(settings hda.AppSettings) error {
	return b.service.SaveSettings(settings)
}

// LoadSettings 返回上次会话的界面状态, 未保存过时为零值。
func (b *SettingsBinding) LoadSettings() (hda.AppSettings, error) {
	return b.service.LoadSettings()
}
