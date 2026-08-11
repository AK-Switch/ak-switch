package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status [provider]",
	Short: "Show runtime status",
	Long:  `Query the running akswitch server and display health, key counts, and request statistics.` + "\n" + `Optional provider name filters output to a single provider.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := NewAdminClient(3*time.Second, "")
		if err != nil {
			return err
		}

		providerName := ""
		if len(args) > 0 {
			providerName = args[0]
		}

		healthPath := "/health"
		if providerName != "" {
			healthPath += "?provider=" + url.QueryEscape(providerName)
		}
		healthReq, err := http.NewRequest(http.MethodGet, client.baseURL+healthPath, nil)
		if err != nil {
			return fmt.Errorf("server not reachable at %s: %w", client.baseURL+healthPath, err)
		}
		healthResp, err := client.Do(healthReq)
		if err != nil {
			return fmt.Errorf("server not reachable at %s: %w", client.baseURL+healthPath, err)
		}
		defer func() { _ = healthResp.Body.Close() }()

		body, _ := io.ReadAll(healthResp.Body)
		if healthResp.StatusCode == http.StatusUnauthorized || healthResp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("auth failed (HTTP %d): check X-Admin-Token in server config", healthResp.StatusCode)
		}
		if healthResp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned (HTTP %d): %s", healthResp.StatusCode, strings.TrimSpace(string(body)))
		}
		var healthData map[string]interface{}
		if err := json.Unmarshal(body, &healthData); err != nil {
			if len(body) > 0 && body[0] != '{' && body[0] != '[' {
				return fmt.Errorf("server not running or returned unexpected response (HTTP %d)", healthResp.StatusCode)
			}
			return fmt.Errorf("failed to parse health response: %w", err)
		}

		fmt.Printf("Server: %s\n", client.baseURL)
		fmt.Printf("Status: %s\n", healthData["status"])

		if providers, ok := healthData["providers"]; ok {
			fmt.Printf("Providers: %v\n", providers)
		}

		if details, ok := healthData["details"]; ok {
			if det, ok2 := details.(map[string]interface{}); ok2 {
				fmt.Print(formatProviderTable(det))
			}
		}

		// Query stats endpoint
		statsPath := "/api/stats"
		if providerName != "" {
			statsPath += "?provider=" + url.QueryEscape(providerName)
		}
		statsResp, err := client.Get(statsPath)
		if err != nil {
			return fmt.Errorf("server not reachable at %s: %w", client.baseURL+statsPath, err)
		}
		defer func() { _ = statsResp.Body.Close() }()

		statsBody, _ := io.ReadAll(statsResp.Body)
		if statsResp.StatusCode == http.StatusUnauthorized || statsResp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("auth failed (HTTP %d): check X-Admin-Token in server config", statsResp.StatusCode)
		}
		if statsResp.StatusCode != http.StatusOK {
			return fmt.Errorf("stats endpoint returned unexpected response (HTTP %d)", statsResp.StatusCode)
		}
		var stats map[string]interface{}
		if err := json.Unmarshal(statsBody, &stats); err == nil {
			fmt.Printf("Requests: %v (success: %v, failed: %v)\n",
				stats["total_requests"], stats["successful_requests"], stats["failed_requests"])
			fmt.Printf("Active keys: %v, Cooling: %v, Disabled: %v\n",
				stats["active_keys"], stats["cooling_keys"], stats["disabled_keys"])
			fmt.Printf("Uptime: %vs\n", stats["uptime_seconds"])
		}

		return nil
	},
}

// formatProviderTable formats the provider details map as a tab-aligned table.
// Exported for testing.
func formatProviderTable(det map[string]interface{}) string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 3, ' ', 0)
	_, _ = fmt.Fprintln(w, "PROVIDER\tKEYS\tCB_STATE")
	for name, info := range det {
		if inf, ok3 := info.(map[string]interface{}); ok3 {
			_, _ = fmt.Fprintf(w, "%s\t%v\t%v\n",
				name, inf["keys"], inf["upstream_cb_state"])
		}
	}
	_ = w.Flush()
	return buf.String()
}
