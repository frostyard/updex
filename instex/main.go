package main

import (
	"os"

	"github.com/frostyard/updex/cmd/instex"
)

func main() {
	if err := instex.Execute(); err != nil {
		os.Exit(1)
	}
}
