package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"hda_client/internal/hda"
)

// JSONStore 把上次会话的界面状态持久化到 JSON 文件(原子写: 临时文件 + rename)。
type JSONStore struct {
	path string
}

func NewJSONStore(path string) *JSONStore {
	return &JSONStore{path: path}
}

func (j *JSONStore) Save(settings hda.AppSettings) error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(j.path), ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o644); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, j.path); err != nil {
		return fmt.Errorf("替换配置文件失败: %w", err)
	}
	committed = true
	return nil
}

// Load 读取界面状态。旧版本以 QueryConfig 结构保存的文件会因未知字段
// 解析失败, 此时按"未保存过"处理, 下次保存即完成迁移。
func (j *JSONStore) Load() (hda.AppSettings, bool, error) {
	data, err := os.ReadFile(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			return hda.AppSettings{}, false, nil
		}
		return hda.AppSettings{}, false, err
	}
	var settings hda.AppSettings
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&settings); err != nil {
		return hda.AppSettings{}, false, fmt.Errorf("解析配置文件 %q: %w", j.path, err)
	}
	if err := ensureEOF(dec); err != nil {
		return hda.AppSettings{}, false, fmt.Errorf("解析配置文件 %q: %w", j.path, err)
	}
	return settings, true, nil
}

func ensureEOF(dec *json.Decoder) error {
	var extra interface{}
	if err := dec.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("配置文件只能包含一个 JSON 对象")
}
