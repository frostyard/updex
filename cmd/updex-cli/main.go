package main

import (
	"os"

	"github.com/frostyard/updex/cmd/updex"
)

// version is set by ldflags during build
var version = "dev"
var commit = "none"
var date = "unknown"
var builtBy = "local"

func main() {
	updex.SetVersion(version)
	updex.SetCommit(commit)
	updex.SetDate(date)
	updex.SetBuiltBy(builtBy)
	if err := updex.Execute(); err != nil {
		os.Exit(1)
	}
}
