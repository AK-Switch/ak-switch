package cli

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// triggerReload sends a reload request to the running server.
// Returns true if the server accepted the signal, false if unreachable.
func triggerReload() bool {
	port := detectServerPort()

	client := &http.Client{Timeout: 3 * time.Second}
	url := fmt.Sprintf("http://%s:%d/api/reload", detectServerHost(), port)
	resp, err := client.Post(url, "application/json", nil)
	if err != nil {
		return false
	}
	resp.Body.Close()
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
