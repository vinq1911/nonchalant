// If you are AI: This script enforces that all files have AI headers and all functions have comments.

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// main checks all Go files in the given directory for required headers and function comments.
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

		// Skip test files (they may not need headers)
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		// Check for AI header
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		content := string(data)
		if !strings.Contains(content, "If you are AI:") {
			failures = append(failures, fmt.Sprintf("%s: missing 'If you are AI:' header", path))
		}

		// Parse and check function comments
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, content, parser.ParseComments)
		if err != nil {
			// Skip files that don't parse (might be generated)
			return nil
		}

		ast.Inspect(f, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				// Skip if it's a test function
				if strings.HasPrefix(fn.Name.Name, "Test") || strings.HasPrefix(fn.Name.Name, "Benchmark") {
					return true
				}

				// Check if function has a comment
				if fn.Doc == nil || len(fn.Doc.List) == 0 {
					pos := fset.Position(fn.Pos())
					failures = append(failures, fmt.Sprintf("%s:%d: function %s missing comment", path, pos.Line, fn.Name.Name))
				}
			}
			return true
		})

		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking directory: %v\n", err)
		os.Exit(1)
	}

	if len(failures) > 0 {
		fmt.Fprintf(os.Stderr, "Comment violations:\n")
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		os.Exit(1)
	}
}
