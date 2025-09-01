package main

import (
	"fmt"
	"os"

	"github.com/symfony-cli/console"
)

func main() {
	app := &console.Application{
		Name:  "fileferry",
		Usage: "Organize media files according to config",
		Flags: []console.Flag{
			&console.BoolFlag{Name: "ack", Usage: "Actually move files"},
		},
		Commands: []*console.Command{listCmd, runCmd},
		Action: func(ctx *console.Context) error {
			// Default to `list` when no subcommand is provided.
			args := ctx.Args()
			if !args.Present() {
				return console.ShowAppHelpAction(ctx)
			}

			// If first argument looks like a subcommand (run or list), let the framework handle it.
			first := args.Slice()[0]
			if first == "run" || first == "list" {
				return console.ShowAppHelpAction(ctx)
			}

			// Otherwise treat the first arg as config path and run the default list behavior.
			cfg, err := LoadConfig(first)
			if err != nil {
				return console.Exit(fmt.Sprintf("Failed to load config: %v", err), 1)
			}
			return performRun(cfg, ctx.Bool("ack"))
		},
	}

	app.Run(os.Args)
}
