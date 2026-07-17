package config

import (
	"bytes"
	"fmt"
	"os"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

// ---- TOML 配置支持 ----

// TomlConfig 对应整个 config.toml 文件结构。
type TomlConfig struct {
	Port            int                  `toml:"port,omitempty"`
	Host            string               `toml:"host,omitempty"`
	DefaultProvider string               `toml:"default_provider,omitempty"`
	LogFile         string               `toml:"log_file,omitempty"`
	LogMaxSize      int                  `toml:"log_max_size,omitempty"`
	LogMaxAge       int                  `toml:"log_max_age,omitempty"`
	Provider        map[string]*Config `toml:"provider"`
}

// DefaultProviderName 保存从 TOML 配置中读取的默认 provider 名称。
// 由 LoadAllTomlProviders 设置，被 start 命令用于确定要启动的 provider。
var DefaultProviderName string

// LoadToml 读取 TOML 配置文件并转换为 Config。
// 文件必须存在且格式合法；格式错误或缺少 [provider] 段返回 error。
func LoadToml(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &ConfigError{
			Category: "system",
			Message:  fmt.Sprintf("系统错误: 读取 TOML 配置文件 %q 失败: %v", path, err),
		}
	}
	var tc TomlConfig
	if err := toml.Unmarshal(data, &tc); err != nil {
		return nil, &ConfigError{
			Category: "config",
			Message:  fmt.Sprintf("配置错误: TOML 解析失败: %v", err),
		}
	}
	DefaultProviderName = tc.DefaultProvider
	if len(tc.Provider) == 0 {
		return nil, &ConfigError{
			Category: "config",
			Message:  "配置错误: TOML 配置缺少 [provider] 段",
		}
	}
	// 取第一个 provider 作为主配置（按名称排序确保确定性）
	names := make([]string, 0, len(tc.Provider))
	for n := range tc.Provider {
		names = append(names, n)
	}
	sort.Strings(names)
	cfg := tc.Provider[names[0]]
	if cfg == nil {
		cfg = DefaultConfig()
	} else {
		mergeConfig(cfg)
	}
	// Port 来自顶层 TOML 配置
	port := tc.Port
	if port == 0 {
		port = DefaultConfig().Port
	}
	cfg.Port = port
	return cfg, nil
}

// SaveToml 将 Config 写入 TOML 文件。覆盖已存在的文件。
func SaveToml(cfg *Config, path string) error {
	tc := &TomlConfig{
		Port:            cfg.Port,
		DefaultProvider: DefaultProviderName,
		Provider:        map[string]*Config{"default": cfg},
	}
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(tc); err != nil {
		return fmt.Errorf("TOML 编码失败: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

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
