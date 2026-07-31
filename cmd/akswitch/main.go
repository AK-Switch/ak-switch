package main

import (
	_ "embed"
	"os"

	"akswitch/internal/cli"
)

//go:embed dashboard.html
var dashboardHTML string

func main() {
	if err := cli.Execute(dashboardHTML); err != nil {
		os.Exit(1)
	}
}