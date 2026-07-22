package main

import (
	_ "embed"

	"akswitch/internal/cli"
)

//go:embed dashboard.html
var dashboardHTML string

func main() {
	cmd.Execute(dashboardHTML)
}