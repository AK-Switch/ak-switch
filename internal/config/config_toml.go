package config

import (
	"bytes"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

// ---- TOML 配置支持 ----

// TomlConfig 对应整个 config.toml 文件结构。
type TomlConfig struct {
	Port            int                  `toml:"port"`
	Host            string               `toml:"host,omitempty"`
	DefaultProvider string               `toml:"default_provider,omitempty"`
	LogFile         string               `toml:"log_file,omitempty"`
	LogMaxSize      int                  `toml:"log_max_size"`
	LogMaxAge       int                  `toml:"log_max_age"`
	Provider        map[string]*Config `toml:"provider"`
}

// DefaultProviderName 保存从 TOML 配置中读取的默认 provider 名称。
// 由 LoadAllTomlProviders 设置，被 start 命令用于确定要启动的 provider。
var DefaultProviderName string

// LoadTomlConfig 读取 TOML 配置文件并返回完整的 TomlConfig（包含所有 provider）。
// 文件不存在时返回原始错误（可通过 os.IsNotExist 检查）。
func LoadTomlConfig(path string) (*TomlConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tc TomlConfig
	if err := toml.Unmarshal(data, &tc); err != nil {
		return nil, err
	}
	warnDeprecatedKeys(data)
	if tc.Provider == nil {
		tc.Provider = make(map[string]*Config)
	}
	return &tc, nil
}

// SaveTomlConfig 将完整 TomlConfig 写入 TOML 文件。覆盖已存在的文件。
func SaveTomlConfig(tc *TomlConfig, path string) error {
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(tc); err != nil {
		return fmt.Errorf("TOML 编码失败: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}
