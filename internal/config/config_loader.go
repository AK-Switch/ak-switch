package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// XDGConfigPath 返回 ~/.akswitch/config.toml 配置路径。
// 可通过 ConfigDir 变量（Go 层面）或 AKSWITCH_CONFIG_DIR 环境变量覆盖。
var ConfigDir string

func XDGConfigPath() (string, error) {
	if ConfigDir != "" {
		return filepath.Join(ConfigDir, "config.toml"), nil
	}
	if configDir := os.Getenv("AKSWITCH_CONFIG_DIR"); configDir != "" {
		return filepath.Join(configDir, "config.toml"), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户家目录失败: %w", err)
	}
	return filepath.Join(homeDir, ".akswitch", "config.toml"), nil
}

// DetectConfigSource 按优先级自动检测配置源。
// 优先级: specifiedPath > XDG config.toml > .env
// 返回检测到的路径、是否为 TOML 源、错误信息。
func DetectConfigSource(specifiedPath string) (source string, fromToml bool, err error) {
	if specifiedPath != "" {
		return specifiedPath, strings.HasSuffix(specifiedPath, ".toml"), nil
	}
	// 检查 XDG 配置路径是否存在
	xdgPath, xdgErr := XDGConfigPath()
	if xdgErr == nil {
		if _, statErr := os.Stat(xdgPath); statErr == nil {
			return xdgPath, true, nil
		}
	}
	// 回退到 .env
	return ".env", false, nil
}

// LoadAllTomlProviders 读取 TOML 配置文件中的所有 [provider.*] 段，
// 每个段转换为独立的 *Config 实例，返回 provider 名到 Config 的映射。
// 文件必须存在且格式合法；格式错误或缺少 [provider] 段返回 error。
func LoadAllTomlProviders(path string) (map[string]*Config, error) {
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
	result := make(map[string]*Config, len(tc.Provider))
	port := tc.Port
	if port == 0 {
		port = DefaultConfig().Port
	}
	for name, p := range tc.Provider {
		cfg := tomlToConfig(name, &p, port)
		// Top-level log fields override per-provider log fields
		if tc.LogFile != "" {
			cfg.LogFile = tc.LogFile
		}
		if tc.LogMaxSize > 0 {
			cfg.LogMaxSize = tc.LogMaxSize
		}
		if tc.LogMaxAge > 0 {
			cfg.LogMaxAge = tc.LogMaxAge
		}
		result[name] = cfg
	}
	return result, nil
}

// FindServerPort finds the first non-zero port from TOML providers.
// Returns 0 if no port is configured or if the TOML file cannot be loaded.
func FindServerPort(xdgPath string) int {
	providers, err := LoadAllTomlProviders(xdgPath)
	if err != nil {
		return 0
	}
	for _, cfg := range providers {
		if cfg.Port > 0 {
			return cfg.Port
		}
	}
	return 0
}