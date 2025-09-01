package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// performRun executes the file iteration and optionally moves files.
func performRun(cfg *Config, ack bool) error {
	skipped := 0
	moved := 0

	for file := range FileIterator(cfg) {
		if file.Error != nil {
			fmt.Printf("%s: %v\n", file.OldPath, file.Error)
			skipped++
			continue
		}

		if !file.ShouldOp {
			skipped++
			continue
		}

		if ack {
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
	return nil
}
