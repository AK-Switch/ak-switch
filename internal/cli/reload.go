package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// triggerReload sends a reload request to the running server.
// Returns true if the server accepted the signal, false if unreachable or auth failed.
func triggerReload() bool {
	client, err := NewAdminClient(3*time.Second, "")
	if err != nil {
		return false
	}

	resp, err := client.Post("/api/reload", "application/json", nil)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		fmt.Fprintf(os.Stderr, "reload auth failed (HTTP %d): check X-Admin-Token in server config\n", resp.StatusCode)
		return false
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "reload failed (HTTP %d): %s\n", resp.StatusCode, string(body))
		return false
	}
	return true
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
