// Command gen-cli-docs generates CLI reference documentation from the akswitch command tree.
//
// Usage:
//
//	go run ./tools/gen-cli-docs
//
// Output is written to docs/cli/ in the project root.
// Run this after adding or modifying any CLI command or flag.
package main

import (
	"fmt"
	"log"
	"os"

	"akswitch/internal/cmd"
)

func main() {
	// Default output directory relative to project root
	outDir := "docs/cli"

	// Allow override via command-line argument
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Fatalf("failed to create output directory %q: %v", outDir, err)
	}

	if err := cmd.GenerateDocs(outDir); err != nil {
		log.Fatalf("failed to generate docs: %v", err)
	}

	fmt.Printf("CLI documentation generated to %s/\n", outDir)
}