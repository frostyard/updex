package main

import (
	"os"

	"github.com/frostyard/updex/cmd/updex"
)

func main() {
	if err := updex.Execute(); err != nil {
		os.Exit(1)
	}
}
