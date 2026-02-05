// If you are AI: This script enforces the 300-line limit on all Go source files.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxLines = 300

// main checks all Go files in the given directory to ensure they don't exceed the line limit.
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <directory>\n", os.Args[0])
		os.Exit(1)
	}

	root := os.Args[1]
	var failures []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only check Go source files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip vendor and testdata directories
		if strings.Contains(path, "/vendor/") || strings.Contains(path, "/testdata/") {
			return nil
		}

		// Count lines
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		lines := strings.Count(string(data), "\n")
		if lines > maxLines {
			failures = append(failures, fmt.Sprintf("%s: %d lines (max %d)", path, lines, maxLines))
		}

		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking directory: %v\n", err)
		os.Exit(1)
	}

	if len(failures) > 0 {
		fmt.Fprintf(os.Stderr, "Files exceeding %d lines:\n", maxLines)
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		os.Exit(1)
	}
}
