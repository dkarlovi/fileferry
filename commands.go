package main

import (
	"fmt"

	"github.com/symfony-cli/console"
)

var listCmd = &console.Command{
	Category:    "",
	Name:        "list",
	Usage:       "Show what would be moved (dry-run)",
	Description: "Scans sources and prints planned target paths without moving files",
	Flags:       []console.Flag{},
	Action: func(c *console.Context) error {
		args := c.Args().Slice()
		if len(args) == 0 {
			return console.Exit("missing config path", 1)
		}
		cfg, err := LoadConfig(args[0])
		if err != nil {
			return console.Exit(fmt.Sprintf("Failed to load config: %v", err), 1)
		}
		return performRun(cfg, false)
	},
}

var runCmd = &console.Command{
	Category:    "",
	Name:        "run",
	Usage:       "Execute moves according to config",
	Description: "Scans sources and moves files according to the target template",
	Flags:       []console.Flag{},
	Action: func(c *console.Context) error {
		args := c.Args().Slice()
		if len(args) == 0 {
			return console.Exit("missing config path", 1)
		}
		cfg, err := LoadConfig(args[0])
		if err != nil {
			return console.Exit(fmt.Sprintf("Failed to load config: %v", err), 1)
		}
		return performRun(cfg, true)
	},
}
