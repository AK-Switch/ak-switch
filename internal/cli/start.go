package cli

import (
	"log/slog"
	"os"

	"akswitch/internal/server"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the API key rotation proxy server",
	Long:  "Loads TOML configuration, initializes the key pool, and starts the HTTP proxy server on a single port with path-based provider routing.",
	Run: func(cmd *cobra.Command, args []string) {
		providerFilter, _ := cmd.Flags().GetString("provider")
		startAll, _ := cmd.Flags().GetBool("all")
		devMode, _ := cmd.Flags().GetBool("dev")
		logFormat, _ := cmd.Flags().GetString("log-format")
		logLevel, _ := cmd.Flags().GetString("log-level")

		restartLogFormat = logFormat

		var rc server.RestartController
		if devMode {
			rc = &selfRestartCtrl{}
		}

		sl := server.NewServerLauncher(dashHTML, providerFilter, logFormat, logLevel, startAll, devMode)
		sl.SetRestartController(rc)
		if err := sl.Launch(); err != nil {
			slog.Error("failed to start server", "error", err)
			os.Exit(1)
		}
	},
}

// pidFilePath delegates to the server package for the PID file path.
// This preserves the existing selfrestart.go call without modifying it.
func pidFilePath(devMode bool) string {
	sl := &server.ServerLauncher{}
	return sl.PidFilePath(devMode)
}

// selfRestartCtrl implements server.RestartController using selfrestart
// package globals. It bridges the cli package's self-restart logic into
// the server package without creating a circular dependency.
type selfRestartCtrl struct{}

func (c *selfRestartCtrl) Setup(exePath string, sigCh chan os.Signal) {
	SetupSelfRestart(exePath, sigCh)
}

func (c *selfRestartCtrl) ShouldRestart() bool {
	return binaryUpdated
}

func (c *selfRestartCtrl) Execute() {
	ExecRestart()
}

func init() {
	startCmd.Flags().String("provider", "", "Only start the specified provider")
	startCmd.Flags().Bool("all", false, "Start all providers (default: first provider alphabetically, or error if none configured)")
	startCmd.Flags().String("log-format", "compact", "Log output format: default or compact")
	startCmd.Flags().String("log-level", "", "Log level: debug, info, warn, error (overrides config.toml)")
	startCmd.Flags().Bool("dev", false, "Start in development mode with auto-incrementing port")
	rootCmd.AddCommand(startCmd)
}
