package commands

import (
	"fmt"

	ffconfig "github.com/dkarlovi/fileferry/config"
	fffile "github.com/dkarlovi/fileferry/file"
	"github.com/symfony-cli/console"
	"github.com/symfony-cli/terminal"
)

var runCmd = &console.Command{
	Category:    "",
	Name:        "run",
	Usage:       "Execute moves according to config",
	Description: "Scans sources and moves files according to the target template",
	Args: []*console.Arg{
		{Name: "profile", Optional: true, Description: "Profile name to run (optional, runs all profiles if not specified)"},
	},
	Flags: []console.Flag{
		&console.BoolFlag{Name: "ack", Usage: "Actually move files"},
	},
	Action: func(c *console.Context) error {
		cfg, err := ffconfig.LoadConfigPrefer(c.String("config"))
		if err != nil {
			return console.Exit(fmt.Sprintf("Failed to load config: %v", err), 1)
		}

		// Get the optional profile argument
		profileName := c.Args().Get("profile")
		// Validate that the profile exists if specified
		if profileName != "" {
			if _, exists := cfg.Profiles[profileName]; !exists {
				return console.Exit(fmt.Sprintf("Profile %q not found in config", profileName), 1)
			}
		}

		skipped := 0
		moved := 0
		deduped := 0
		errors := 0

		// detect verbose mode (-v)
		verbose := terminal.IsVerbose()

		filesCh, evCh, sources := fffile.FileIteratorWithEvents(cfg, profileName)
		// Keep source sessions (e.g. an MTP device connection) alive until all
		// moves are done; entries' Open/Delete rely on them.
		defer sources.Close()

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
			// when verbose, show the currently scanned file
			if verbose {
				fmt.Fprintf(c.App.Writer, "Scanning file: <comment>%s</>\n", file.OldPath)
			}

			if file.Error != nil {
				// Special handling for unpopulated tokens - treat as skip with warning
				if unpopErr, ok := file.Error.(*fffile.UnpopulatedTokensError); ok {
					fmt.Fprintf(c.App.Writer, "<fg=yellow>Warning: Skipping %s: %v</>\n", file.OldPath, unpopErr)
					skipped++
					continue
				}
				// All other errors go to stderr
				fmt.Fprintf(c.App.ErrWriter, "%s: %v\n", file.OldPath, file.Error)
				errors++
				continue
			}

			if !file.ShouldOp {
				skipped++
				continue
			}

			if c.Bool("ack") {
				fmt.Fprintf(c.App.Writer, "Moving %s -> %s\n", file.OldPath, file.NewPath)
				outcome, err := fffile.MoveEntry(file.Entry, file.NewPath)
				if err != nil {
					return console.Exit(fmt.Sprintf("%s: failed to move: %v", file.OldPath, err), 1)
				}
				if outcome == fffile.Deduplicated {
					fmt.Fprintf(c.App.Writer, "<fg=yellow>Duplicate: %s already exists at %s, deleted source</>\n", file.OldPath, file.NewPath)
					deduped++
				} else {
					moved++
				}
			} else {
				outcome, err := fffile.PreviewMove(file.Entry, file.NewPath)
				if err != nil {
					fmt.Fprintf(c.App.ErrWriter, "%s: %v\n", file.OldPath, err)
					errors++
					continue
				}
				if outcome == fffile.Deduplicated {
					fmt.Fprintf(c.App.Writer, "<fg=yellow>Would skip duplicate: %s already exists at %s</>\n", file.OldPath, file.NewPath)
					deduped++
				} else {
					fmt.Fprintf(c.App.Writer, "Would move %s -> %s (use --ack to actually move)\n", file.OldPath, file.NewPath)
					moved++
				}
			}
		}

		fmt.Fprintf(c.App.Writer, "Summary: %d moved, %d duplicates, %d skipped, %d errors.\n", moved, deduped, skipped, errors)
		return nil
	},
}

func Commands() []*console.Command {
	return []*console.Command{runCmd}
}
