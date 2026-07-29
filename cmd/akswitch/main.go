package main

import (
	_ "embed"

	"akswitch/internal/cli"
)

//go:embed dashboard.html
var dashboardHTML string

func main() {
	_ = cli.Execute(dashboardHTML)
}