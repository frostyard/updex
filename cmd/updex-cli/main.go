package main

import (
	"os"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/cmd/updex"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "local"
)

func main() {
	app := clix.App{
		Version: version,
		Commit:  commit,
		Date:    date,
		BuiltBy: builtBy,
	}
	if err := app.Run(updex.NewRootCmd()); err != nil {
		os.Exit(1)
	}
}
