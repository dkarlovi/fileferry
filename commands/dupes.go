package commands

import (
	"fmt"
	"os"
	"path/filepath"

	ffconfig "github.com/dkarlovi/fileferry/config"
	fffile "github.com/dkarlovi/fileferry/file"
	"github.com/symfony-cli/console"
)

var dupesCmd = &console.Command{
	Category: "tidy",
	Name:     "dupes",
	Aliases:  []*console.Alias{{Name: "dupes"}},
	Usage:    "Find and remove duplicate files in the target trees",
	Description: `Scans the target tree of each profile for byte-identical files sitting side by side
and removes the redundant copies, keeping the one the current config asks for.

Files are grouped by directory and size first, so only same-size siblings are
hashed. Of each identical set, the copy whose own metadata resolves to the path
it already occupies is kept — the result is exactly what a fresh run of this
config would have produced. When no copy is where the config wants it (usually
because the target template changed since they were written), one is kept by
name order and the set is reported as still needing a rename.

Dry-run by default; pass --ack to actually delete.`,
	Args: []*console.Arg{
		{Name: "profile", Optional: true, Description: "Profile name to check (optional, checks all profiles if not specified)"},
	},
	Flags: []console.Flag{
		&console.BoolFlag{Name: "ack", Usage: "Actually delete the redundant copies"},
	},
	Action: func(c *console.Context) error {
		cfg, err := ffconfig.LoadConfigPrefer(c.String("config"))
		if err != nil {
			return console.Exit(fmt.Sprintf("Failed to load config: %v", err), 1)
		}

		reports, err := fffile.FindDupes(cfg, c.Args().Get("profile"))
		if err != nil {
			return console.Exit(err.Error(), 1)
		}

		ack := c.Bool("ack")
		sets := 0
		deleted := 0
		var freed int64
		pendingRename := 0
		errors := 0

		for _, rep := range reports {
			fmt.Fprintf(c.App.Writer, "Checking profile=<info>%s</> <comment>%s</>\n", rep.Profile, rep.Root)
			for _, e := range rep.Errors {
				fmt.Fprintf(c.App.ErrWriter, "<fg=red>Error: %v</>\n", e)
				errors++
			}
			fmt.Fprintf(c.App.Writer, "Scanned <warning>%d</> files in <comment>%s</>\n", rep.Scanned, rep.Root)

			for _, set := range rep.Sets {
				sets++
				fmt.Fprintf(c.App.Writer, "Duplicate in <comment>%s</> (%s, sha256 %s)\n", set.Dir, humanSize(set.Size), set.Hash[:12])
				if set.Reason == fffile.KeepCanonical {
					fmt.Fprintf(c.App.Writer, "  keep   <info>%s</> (matches the target template)\n", filepath.Base(set.Keep))
				} else {
					fmt.Fprintf(c.App.Writer, "  keep   <info>%s</>\n", filepath.Base(set.Keep))
				}

				if set.Reason == fffile.KeepFallback {
					pendingRename++
					if set.Canonical == "" {
						fmt.Fprintf(c.App.Writer, "         <fg=yellow>no copy matches the target template and it cannot be resolved for this file; kept the first by name</>\n")
					} else {
						fmt.Fprintf(c.App.Writer, "         <fg=yellow>no copy is at its target path (%s); kept the first by name</>\n", set.Canonical)
					}
				}

				for _, path := range set.Delete {
					if !ack {
						fmt.Fprintf(c.App.Writer, "  delete <fg=yellow>%s</> (use --ack to actually delete)\n", filepath.Base(path))
						deleted++
						freed += set.Size
						continue
					}
					if err := os.Remove(path); err != nil {
						fmt.Fprintf(c.App.ErrWriter, "<fg=red>Error: delete %s: %v</>\n", path, err)
						errors++
						continue
					}
					fmt.Fprintf(c.App.Writer, "  delete <fg=yellow>%s</>\n", filepath.Base(path))
					deleted++
					freed += set.Size
				}
			}
		}

		verb := "would delete"
		if ack {
			verb = "deleted"
		}
		summary := fmt.Sprintf("Summary: %s, %s %s (%s)", plural(sets, "duplicate set"), verb, plural(deleted, "file"), humanSize(freed))
		if pendingRename > 0 {
			summary += fmt.Sprintf(", %s still need a rename", plural(pendingRename, "set"))
		}
		summary += fmt.Sprintf(", %s", plural(errors, "error"))
		fmt.Fprintf(c.App.Writer, "%s.\n", summary)
		return nil
	},
}

// plural renders a count with its noun, adding a naive "s" for anything but one.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// humanSize renders a byte count in the largest binary unit that keeps it
// readable, e.g. "100.0 MiB".
func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTP"[exp])
}
