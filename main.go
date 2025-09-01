package main

import (
	"os"

	"github.com/dkarlovi/fileferry/commands"
	"github.com/symfony-cli/console"
)

func main() {
	app := &console.Application{
		Name:  "fileferry",
		Usage: "Organize media files according to config",
		Flags: []console.Flag{
			&console.StringFlag{Name: "config", Aliases: []string{"C"}, DefaultValue: "", Usage: "Path to config file (optional)"},
		},
		Commands: commands.Commands(),
	}

	app.Run(os.Args)
}
