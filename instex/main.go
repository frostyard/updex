package main

import (
	"os"

	"github.com/frostyard/updex/cmd/instex"
)

// version is set by ldflags during build
var version = "dev"
var commit = "none"
var date = "unknown"
var builtBy = "local"

func main() {
	instex.SetVersion(version)
	instex.SetCommit(commit)
	instex.SetDate(date)
	instex.SetBuiltBy(builtBy)
	if err := instex.Execute(); err != nil {
		os.Exit(1)
	}
}
