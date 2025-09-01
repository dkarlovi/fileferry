package commands

import (
	"fmt"

	"os"
	"path/filepath"

	"github.com/dkarlovi/fileferry/config"
	filepkg "github.com/dkarlovi/fileferry/file"
	"github.com/symfony-cli/console"
)

var runCmd = &console.Command{
	Category:    "",
	Name:        "run",
	Usage:       "Execute moves according to config",
	Description: "Scans sources and moves files according to the target template",
	Args: []*console.Arg{
		{Name: "config"},
	},
	Flags: []console.Flag{
		&console.BoolFlag{Name: "ack", Usage: "Actually move files"},
	},
	Action: func(c *console.Context) error {
		cfg, err := config.LoadConfig(c.Args().Get("config"))
		if err != nil {
			return console.Exit(fmt.Sprintf("Failed to load config: %v", err), 1)
		}
		return performRun(cfg, c.Bool("ack"))
	},
}

func Commands() []*console.Command {
	return []*console.Command{runCmd}
}

func performRun(cfg *config.Config, ack bool) error {
	skipped := 0
	moved := 0

	for file := range filepkg.FileIterator(cfg) {
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
