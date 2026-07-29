package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show runtime status",
	Long:  `Query the running akswitch server and display health, key counts, and request statistics.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := &http.Client{Timeout: 3 * time.Second}

		// Determine the server port from config or default
		port := detectServerPort()

		// Query health endpoint on the server port
		healthURL := fmt.Sprintf("http://%s:%d/health", detectServerHost(), port)
		healthReq, err := http.NewRequest(http.MethodGet, healthURL, nil)
		if err != nil {
			return fmt.Errorf("server not reachable at %s: %w", healthURL, err)
		}
		if token, tokErr := loadAdminTokenFromConfig(); tokErr == nil && token != "" {
			healthReq.Header.Set("X-Admin-Token", token)
		}

		resp, err := client.Do(healthReq)
		if err != nil {
			return fmt.Errorf("server not reachable at %s: %w", healthURL, err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("auth failed (HTTP %d): check X-Admin-Token in server config", resp.StatusCode)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server not running or returned unexpected response (HTTP %d)", resp.StatusCode)
		}
		var healthData map[string]interface{}
		if err := json.Unmarshal(body, &healthData); err != nil {
			// Check if response is non-JSON (e.g., HTML from another service)
			if len(body) > 0 && body[0] != '{' && body[0] != '[' {
				return fmt.Errorf("server not running or returned unexpected response (HTTP %d)", resp.StatusCode)
			}
			return fmt.Errorf("failed to parse health response: %w", err)
		}

		fmt.Printf("Server: http://%s:%d\n", detectServerHost(), port)
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
		statsURL := fmt.Sprintf("http://%s:%d/api/stats", detectServerHost(), port)
		statsReq, err := http.NewRequest(http.MethodGet, statsURL, nil)
		if err != nil {
			return fmt.Errorf("server not reachable at %s: %w", statsURL, err)
		}
		if token, tokErr := loadAdminTokenFromConfig(); tokErr == nil && token != "" {
			statsReq.Header.Set("X-Admin-Token", token)
		}

		statsResp, err := client.Do(statsReq)
		if err != nil {
			return fmt.Errorf("server not reachable at %s: %w", statsURL, err)
		}
		defer statsResp.Body.Close()

		statsBody, _ := io.ReadAll(statsResp.Body)
		if statsResp.StatusCode == http.StatusUnauthorized || statsResp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("auth failed (HTTP %d): check X-Admin-Token in server config", statsResp.StatusCode)
		}
		if statsResp.StatusCode != http.StatusOK {
			return fmt.Errorf("stats endpoint returned unexpected response (HTTP %d)", statsResp.StatusCode)
		}
		if err == nil {
			var stats map[string]interface{}
			if err := json.Unmarshal(statsBody, &stats); err == nil {
				fmt.Printf("Requests: %v (success: %v, failed: %v)\n",
					stats["total_requests"], stats["successful_requests"], stats["failed_requests"])
				fmt.Printf("Active keys: %v, Cooling: %v, Disabled: %v\n",
					stats["active_keys"], stats["cooling_keys"], stats["disabled_keys"])
				fmt.Printf("Uptime: %vs\n", stats["uptime_seconds"])
			}
		}
		return nil
	},
}

// formatProviderTable formats the provider details map as a tab-aligned table.
// Exported for testing.
func formatProviderTable(det map[string]interface{}) string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tKEYS\tCB_STATE")
	for name, info := range det {
		if inf, ok3 := info.(map[string]interface{}); ok3 {
			fmt.Fprintf(w, "%s\t%v\t%v\n",
				name, inf["keys"], inf["upstream_cb_state"])
		}
	}
	w.Flush()
	return buf.String()
}