package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: fileferry <config.yaml> [--ack]")
		os.Exit(1)
	}
	ack := false
	configPath := ""
	for _, arg := range os.Args[1:] {
		if arg == "--ack" {
			ack = true
		} else if configPath == "" {
			configPath = arg
		}
	}
	if configPath == "" {
		fmt.Println("Usage: fileferry <config.yaml> [--ack]")
		os.Exit(1)
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Loaded config: %+v\n", cfg)

	skipped := 0
	moved := 0

	// Process files using the iterator
	for file := range FileIterator(cfg) {
		// Handle errors during file processing
		if file.Error != nil {
			fmt.Printf("%s: %v\n", file.OldPath, file.Error)
			skipped++
			continue
		}

		// Skip files that don't need operations
		if !file.ShouldOp {
			skipped++
			continue
		}

		// Perform the move operation
		if ack {
			// Create target directory
			dir := filepath.Dir(file.NewPath)
			if err := os.MkdirAll(dir, 0755); err != nil {
				fmt.Printf("%s: failed to create dir %s: %v\n", file.OldPath, dir, err)
				skipped++
				continue
			}

			fmt.Printf("Moving %s -> %s\n", file.OldPath, file.NewPath)
			if err := os.Rename(file.OldPath, file.NewPath); err != nil {
				fmt.Printf("%s: failed to move: %v\n", file.OldPath, err)
				skipped++
				continue
			}
			moved++
		} else {
			fmt.Printf("Would move %s -> %s (use --ack to actually move)\n", file.OldPath, file.NewPath)
			moved++
		}
	}

	fmt.Printf("Summary: %d moved, %d skipped.\n", moved, skipped)
}
