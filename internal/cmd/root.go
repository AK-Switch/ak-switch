package cmd

import (
	"fmt"

	"akswitch/internal/config"
	"github.com/spf13/cobra"
)

const (
	adminPort = 8080
	adminHost = "127.0.0.1"
)

var Version = "dev"
var dashHTML string

var rootCmd = &cobra.Command{
	Use:           "akswitch",
	Short:         "API Key rotation proxy for AI providers",
	SilenceUsage:  true,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("akswitch version %s\n", rootCmd.Version)
	},
}

func Execute(dashboardHTML string) error {
	dashHTML = dashboardHTML
	return rootCmd.Execute()
}

// detectServerPort returns the server port from the config file,
// falling back to the default admin port if detection fails.
func detectServerPort() int {
	if xdgPath, err := config.XDGConfigPath(); err == nil {
		if p := config.FindServerPort(xdgPath); p > 0 {
			return p
		}
	}
	return adminPort
}

// detectServerHost returns the server host from the config file,
// falling back to the default admin host if detection fails.
func detectServerHost() string {
	if xdgPath, err := config.XDGConfigPath(); err == nil {
		if h := config.FindServerHost(xdgPath); h != "" {
			return h
		}
	}
	return adminHost
}

func init() {
	rootCmd.Version = Version
	rootCmd.AddCommand(versionCmd)
}