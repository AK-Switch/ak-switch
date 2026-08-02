package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"akswitch/internal/config"

	"github.com/spf13/cobra"
)

// credentialEntry represents a single line in the auto-reg-bot JSONL credential file.
type credentialEntry struct {
	APIKey       string `json:"api_key"`
	APIKeyPlain  string `json:"api_key_plain"`
	Account      string `json:"account"`
	Password     string `json:"password"`
	AccountName  string `json:"account_name,omitempty"`
	TenantID     string `json:"tenant_id,omitempty"`
}

func init() {
	rootCmd.AddCommand(usageCmd)
	usageCmd.Flags().StringP("key", "k", "", "API key to query (required)")
	usageCmd.Flags().StringP("provider", "p", "", "Provider name (default: configured default provider)")
	usageCmd.Flags().String("credentials-dir", "", "Path to credentials directory (overrides config)")
}

var usageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Query API key remaining usage quota",
	Long: `Query the remaining usage quota for an API key by looking up its
associated account credentials, authenticating, and calling the provider's
usage API.

Currently supported providers: sensenova

Example:
  akswitch usage --key sk-xxxxxxxxxxxxxxxx --provider sensenova
  akswitch usage --key sk-xxxxxxxxxxxxxxxx --provider sensenova --credentials-dir ./creds`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		apiKey, _ := cmd.Flags().GetString("key")
		if apiKey == "" {
			return fmt.Errorf("--key is required")
		}

		providerName, _ := cmd.Flags().GetString("provider")
		if providerName == "" {
			providerName = config.DefaultProviderName
		}
		if providerName == "" {
			return fmt.Errorf("--provider is required (no default provider configured)")
		}

		credsDir, _ := cmd.Flags().GetString("credentials-dir")
		if credsDir == "" {
			xdgPath, err := config.XDGConfigPath()
			if err != nil {
				return fmt.Errorf("cannot determine config path: %w", err)
			}
			tc, err := config.LoadTomlConfig(xdgPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			credsDir = tc.CredentialsDir
			if credsDir == "" {
				credsDir = "credentials"
			}
		}

		return queryUsage(providerName, apiKey, credsDir)
	},
}

// queryUsage looks up credentials, authenticates, and queries the usage API.
func queryUsage(provider, apiKey, credsDir string) error {
	switch provider {
	case "sensenova":
		return querySensenovaUsage(apiKey, credsDir)
	default:
		return fmt.Errorf("usage query not supported for provider %q (supported: sensenova)", provider)
	}
}

func querySensenovaUsage(apiKey, credsDir string) error {
	// 1. Find credential file: <credsDir>/sensenova_credentials.jsonl
	credFile := filepath.Join(credsDir, "sensenova_credentials.jsonl")
	data, err := os.ReadFile(credFile)
	if err != nil {
		return fmt.Errorf("failed to read credentials file %q: %w\n  Hint: use --credentials-dir to specify the path", credFile, err)
	}

	// 2. Find the entry matching the API key
	entry, err := findCredentialByKey(data, apiKey)
	if err != nil {
		return err
	}

	fmt.Printf("Found account: %s (tenant: %s)\n", entry.AccountName, entry.TenantID)

	// 3. Login to get access token
	token, err := sensenovaLoginWithURL(entry.Account, entry.Password, "https://platform.sensenova.cn/api/iam/v3/user/login")
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	// 4. Query usage API
	usages, err := sensenovaQueryUsageWithURL(token, entry.TenantID, "https://platform.sensenova.cn/lite/console/v1/user/coding-plan/usages")
	if err != nil {
		return err
	}

	// 5. Display results
	fmt.Printf("\nRemaining quota:\n")
	for model, pct := range usages {
		bar := usageBar(pct)
		fmt.Printf("  %-30s %6.1f%% %s\n", model, pct, bar)
	}
	return nil
}

// findCredentialByKey scans JSONL data for an entry whose api_key matches.
func findCredentialByKey(data []byte, targetKey string) (*credentialEntry, error) {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry credentialEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		key := entry.APIKey
		if key == "" {
			key = entry.APIKeyPlain
		}
		if key == targetKey {
			return &entry, nil
		}
	}
	return nil, fmt.Errorf("API key not found in credentials file")
}

// sensenovaLogin authenticates with account/password and returns an access token.
var sensenovaLoginWithURL = func(account, password, baseURL string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	payload := map[string]string{
		"account":  account,
		"password": password,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal login payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read login response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse login response: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("login returned empty access_token")
	}
	return result.AccessToken, nil
}

// sensenovaQueryUsage queries the coding-plan usage endpoint.
var sensenovaQueryUsageWithURL = func(token, tenantID, baseURL string) (map[string]float64, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	url := fmt.Sprintf("%s?account_id=%s", baseURL, tenantID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("usage request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read usage response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("usage HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var result struct {
		ModelRemainingPercent map[string]float64 `json:"model_remaining_percent"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse usage response: %w", err)
	}
	if result.ModelRemainingPercent == nil {
		return nil, fmt.Errorf("usage API returned empty data")
	}
	return result.ModelRemainingPercent, nil
}

// usageBar returns a simple text bar representing a percentage.
func usageBar(pct float64) string {
	if pct >= 99.5 {
		return "[||||||||||]"
	}
	if pct >= 80 {
		return "[||||||||| ]"
	}
	if pct >= 50 {
		return "[||||||||  ]"
	}
	if pct >= 20 {
		return "[||||||    ]"
	}
	if pct > 0 {
		return "[||||      ]"
	}
	return "[          ]"
}

// truncate limits a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

