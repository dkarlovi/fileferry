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
		Commands: commands.Commands(),
	}

	app.Run(os.Args)
}
