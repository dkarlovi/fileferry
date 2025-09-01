package commands

import (
	"fmt"

	"os"
	"path/filepath"

	ffconfig "github.com/dkarlovi/fileferry/config"
	fffile "github.com/dkarlovi/fileferry/file"
	"github.com/symfony-cli/console"
)

var runCmd = &console.Command{
	Category:    "",
	Name:        "run",
	Usage:       "Execute moves according to config",
	Description: "Scans sources and moves files according to the target template",
	Flags: []console.Flag{
		&console.BoolFlag{Name: "ack", Usage: "Actually move files"},
	},
	Action: func(c *console.Context) error {
		cfg, err := ffconfig.LoadConfigPrefer(c.String("config"))
		if err != nil {
			return console.Exit(fmt.Sprintf("Failed to load config: %v", err), 1)
		}

		skipped := 0
		moved := 0
		errors := 0

		filesCh, evCh := fffile.FileIteratorWithEvents(cfg)

		// consume events and print colored messages (profile and path highlighted)
		go func() {
			for ev := range evCh {
				switch ev.EventType {
				case "start":
					fmt.Fprintf(c.App.Writer, "Scanning profile=<info>%s</> <comment>%s</> (recurse=%v, types=%v)\n", ev.Profile, ev.SrcPath, ev.Recurse, ev.Types)
				case "found":
					fmt.Fprintf(c.App.Writer, "Found <warning>%d</> files in <comment>%s</>\n", ev.Found, ev.SrcPath)
				case "error":
					fmt.Fprintf(c.App.ErrWriter, "<fg=red>Error scanning %s: %v</>\n", ev.SrcPath, ev.Error)
				}
			}
		}()

		for file := range filesCh {
			if file.Error != nil {
				fmt.Fprintf(c.App.ErrWriter, "%s: %v\n", file.OldPath, file.Error)
				errors++
				continue
			}

			if !file.ShouldOp {
				skipped++
				continue
			}

			if c.Bool("ack") {
				dir := filepath.Dir(file.NewPath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					return console.Exit(fmt.Sprintf("%s: failed to create dir %s: %v", file.OldPath, dir, err), 1)
				}
				fmt.Fprintf(c.App.Writer, "Moving %s -> %s\n", file.OldPath, file.NewPath)
				if err := os.Rename(file.OldPath, file.NewPath); err != nil {
					return console.Exit(fmt.Sprintf("%s: failed to move: %v", file.OldPath, err), 1)
				}
				moved++
			} else {
				fmt.Fprintf(c.App.Writer, "Would move %s -> %s (use --ack to actually move)\n", file.OldPath, file.NewPath)
				moved++
			}
		}

		fmt.Fprintf(c.App.Writer, "Summary: %d moved, %d skipped, %d errors.\n", moved, skipped, errors)
		return nil
	},
}

func Commands() []*console.Command {
	return []*console.Command{runCmd}
}
