package main

import (
	"os"

	"github.com/dkarlovi/fileferry/commands"
	"github.com/symfony-cli/console"
)

var (
	// version is overridden at linking time
	version = "dev"
	// buildDate is overridden at linking time
	buildDate string
)

func main() {
	app := &console.Application{
		Name:      "fileferry",
		Usage:     "Organize media files according to config",
		Version:   version,
		BuildDate: buildDate,
		Channel:   "stable",
		Flags: []console.Flag{
			&console.StringFlag{
				Name:         "config",
				Aliases:      []string{"C"},
				DefaultValue: "",
				Usage: `Path to configuration file

Lookup order (first existing path wins):
  1) <info>--config <path></> (explicit)
  2) <info>./config.yaml</> (current working directory)
  3) user config dir: <info>$XDG_CONFIG_HOME/fileferry/config.yaml</> or the OS-specific user config directory (via <info>os.UserConfigDir()</>)`,
			},
		},
		Commands: commands.Commands(),
	}

	app.Run(os.Args)
}
