package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
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
	for name, p := range tc.Provider {
		if p == nil {
			p = DefaultConfig()
		}
		// 1. Propagate global fields first (only fills provider zero-values)
		propagateGlobalFields(&tc, p)
		// 2. Then fill any remaining zero-values with hardcoded defaults
		mergeConfig(p)
		result[name] = p
	}
	// Warn about provider names containing uppercase letters.
	// Provider names are used in route paths, log labels, and metrics;
	// lowercase kebab-case is the recommended convention.
	for name := range tc.Provider {
		if strings.ToLower(name) != name {
			slog.Warn("provider name contains uppercase letters; consider using lowercase kebab-case",
				"provider", name)
		}
	}
	return result, nil
}

// FindServerHost finds the first non-empty host from TOML providers.
// Returns empty string if no host is configured or if the TOML file cannot be loaded.
func FindServerHost(xdgPath string) string {
	providers, err := LoadAllTomlProviders(xdgPath)
	if err != nil {
		return ""
	}
	for _, cfg := range providers {
		if cfg.Host != "" {
			return cfg.Host
		}
	}
	return ""
}

// propagateGlobalFields copies non-zero fields from the global TomlConfig into
// the provider Config, but only when the provider field is still at its zero
// value. This means: top-level values act as defaults, provider-level values
// always win.
//
// The two structs (TomlConfig and Config) share most field names; fields that
// exist only on one side (e.g. DefaultProvider) are skipped automatically.
func propagateGlobalFields(tc *TomlConfig, p *Config) {
	tv := reflect.ValueOf(tc).Elem()
	pv := reflect.ValueOf(p).Elem()
	pt := pv.Type()

	for i := 0; i < pt.NumField(); i++ {
		field := pt.Field(i)
		tomlTag := field.Tag.Get("toml")
		if tomlTag == "-" {
			continue // runtime-only field, not a TOML field
		}

		// Look up the same-named field on TomlConfig
		gv := tv.FieldByName(field.Name)
		if !gv.IsValid() {
			continue // no corresponding global field
		}

		// Only propagate when global value is non-zero AND provider value is zero
		pf := pv.Field(i)
		if !gv.IsZero() && pf.IsZero() && pf.CanSet() {
			pf.Set(gv)
		}
	}
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