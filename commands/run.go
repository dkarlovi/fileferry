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
	Usage:       "Execute transfers according to config",
	Description: "Scans sources and moves or copies files according to the target template",
	Args: []*console.Arg{
		{Name: "profile", Optional: true, Description: "Profile name to run (optional, runs all profiles if not specified)"},
	},
	Flags: []console.Flag{
		&console.BoolFlag{Name: "ack", Usage: "Actually move/copy files"},
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
		copied := 0
		deduped := 0
		errors := 0

		// detect verbose mode (-v)
		verbose := terminal.IsVerbose()

		filesCh, evCh, sources := fffile.FileIteratorWithEvents(cfg, profileName)
		// Keep source sessions (e.g. an MTP device connection) alive until all
		// moves are done; entries' Open/Delete rely on them.
		defer sources.Close()

		// consume events and print colored messages (profile and path highlighted).
		// The counters below belong to this goroutine alone; they are folded into
		// the totals only after evDone signals it has finished.
		scanErrors := 0
		warnings := 0
		evDone := make(chan struct{})
		go func() {
			defer close(evDone)
			for ev := range evCh {
				switch ev.EventType {
				case "start":
					fmt.Fprintf(c.App.Writer, "Scanning profile=<info>%s</> <comment>%s</> (operation=%v, recurse=%v, types=%v)\n", ev.Profile, ev.SrcPath, ev.Operation, ev.Recurse, ev.Types)
				case "found":
					fmt.Fprintf(c.App.Writer, "Found <warning>%d</> files in <comment>%s</>\n", ev.Found, ev.SrcPath)
				case "warning":
					fmt.Fprintf(c.App.Writer, "<fg=yellow>Warning: skipping %s: %v</>\n", ev.SrcPath, ev.Error)
					warnings++
				case "error":
					fmt.Fprintf(c.App.ErrWriter, "<fg=red>Error scanning %s: %v</>\n", ev.SrcPath, ev.Error)
					scanErrors++
				}
			}
		}()

		// reportError is the single place per-file failures are rendered, so the
		// same situation (most notably a destination that exists with different
		// content) looks identical whether it surfaced during a dry run or a real
		// move.
		reportError := func(path string, err error) {
			fmt.Fprintf(c.App.ErrWriter, "<fg=red>Error: %s: %v</>\n", path, err)
			errors++
		}

		for file := range filesCh {
			// when verbose, show the currently scanned file
			if verbose {
				fmt.Fprintf(c.App.Writer, "Scanning file: <comment>%s</>\n", file.OldPath)
			}

			if file.Error != nil {
				// Special handling for unpopulated tokens - treat as skip with warning
				if unpopErr, ok := file.Error.(*fffile.UnpopulatedTokensError); ok {
					fmt.Fprintf(c.App.Writer, "<fg=yellow>Warning: skipping %s: %v</>\n", file.OldPath, unpopErr)
					skipped++
					continue
				}
				// All other errors go to stderr
				reportError(file.OldPath, file.Error)
				continue
			}

			if !file.ShouldOp {
				skipped++
				continue
			}

			isCopy := file.Operation == ffconfig.OperationCopy

			if c.Bool("ack") {
				if isCopy {
					fmt.Fprintf(c.App.Writer, "Copying %s -> %s\n", file.OldPath, file.NewPath)
				} else {
					fmt.Fprintf(c.App.Writer, "Moving %s -> %s\n", file.OldPath, file.NewPath)
				}
				outcome, err := fffile.TransferEntry(file.Entry, file.NewPath, file.Operation)
				if err != nil {
					// A single file failing to transfer (e.g. a destination that
					// exists with different content) must not abort the whole run:
					// record it and keep going with the remaining files.
					reportError(file.OldPath, err)
					continue
				}
				if outcome == fffile.Deduplicated {
					if isCopy {
						fmt.Fprintf(c.App.Writer, "<fg=yellow>Duplicate: %s already exists at %s, left source in place</>\n", file.OldPath, file.NewPath)
					} else {
						fmt.Fprintf(c.App.Writer, "<fg=yellow>Duplicate: %s already exists at %s, deleted source</>\n", file.OldPath, file.NewPath)
					}
					deduped++
				} else if isCopy {
					copied++
				} else {
					moved++
				}
			} else {
				outcome, err := fffile.PreviewTransfer(file.Entry, file.NewPath)
				if err != nil {
					reportError(file.OldPath, err)
					continue
				}
				if outcome == fffile.Deduplicated {
					fmt.Fprintf(c.App.Writer, "<fg=yellow>Would skip duplicate: %s already exists at %s</>\n", file.OldPath, file.NewPath)
					deduped++
				} else if isCopy {
					fmt.Fprintf(c.App.Writer, "Would copy %s -> %s (use --ack to actually copy)\n", file.OldPath, file.NewPath)
					copied++
				} else {
					fmt.Fprintf(c.App.Writer, "Would move %s -> %s (use --ack to actually move)\n", file.OldPath, file.NewPath)
					moved++
				}
			}
		}

		// Wait for the event printer so its output lands before the summary and
		// its counters are safe to read.
		<-evDone
		errors += scanErrors

		summary := fmt.Sprintf("Summary: %d moved, %d duplicates, %d skipped, %d errors", moved, deduped, skipped, errors)
		if copied > 0 {
			summary = fmt.Sprintf("Summary: %d moved, %d copied, %d duplicates, %d skipped, %d errors", moved, copied, deduped, skipped, errors)
		}
		if warnings > 0 {
			summary += fmt.Sprintf(", %d warnings", warnings)
		}
		fmt.Fprintf(c.App.Writer, "%s.\n", summary)
		return nil
	},
}

func Commands() []*console.Command {
	return []*console.Command{runCmd}
}
