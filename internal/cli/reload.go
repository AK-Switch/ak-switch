package cli

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"akswitch/internal/config"

	"github.com/spf13/cobra"
)

// triggerReload sends a reload request to the running server.
// Returns true if the server accepted the signal, false if unreachable or auth failed.
func triggerReload() bool {
	port := detectServerPort()

	client := &http.Client{Timeout: 3 * time.Second}
	url := fmt.Sprintf("http://%s:%d/api/reload", detectServerHost(), port)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return false
	}
	if token, tokErr := loadAdminTokenFromConfig(); tokErr == nil && token != "" {
		req.Header.Set("X-Admin-Token", token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// loadAdminTokenFromConfig loads the admin token from the TOML config file.
// Returns empty string if the config doesn't exist or has no admin token.
func loadAdminTokenFromConfig() (string, error) {
	xdgPath, err := config.XDGConfigPath()
	if err != nil {
		return "", err
	}
	tc, err := config.LoadTomlConfig(xdgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	for _, p := range tc.Provider {
		if p.AdminToken != "" {
			return p.AdminToken, nil
		}
	}
	return "", nil
}

var reloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Reload configuration from disk",
	Long: `Send a reload signal to the running akswitch server to pick up
configuration changes without restart.

This re-reads config.toml and reloads keys from the key store.
Disabled keys remain disabled across reloads.

If the server is not running, the command exits silently.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if triggerReload() {
			fmt.Println("reload signal sent")
		} else {
			fmt.Fprintln(os.Stderr, "server not running — nothing to reload")
			return nil
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(reloadCmd)
}
