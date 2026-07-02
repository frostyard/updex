package updex

import (
	"fmt"

	"github.com/frostyard/clix"
	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/download"
)

// isQuiet reports whether non-error output should be suppressed. It treats the
// clix --silent/-s flag as equivalent to --quiet so both suppress output.
func isQuiet() bool {
	return quiet || clix.Silent
}

// outPrintln writes a line to stdout unless quiet mode is enabled.
func outPrintln(a ...any) {
	if isQuiet() {
		return
	}
	fmt.Println(a...)
}

// outPrintf writes formatted output to stdout unless quiet mode is enabled.
func outPrintf(format string, a ...any) {
	if isQuiet() {
		return
	}
	fmt.Printf(format, a...)
}

// selectReporter returns the SDK progress reporter to use. In quiet mode all
// progress/info/warning output is discarded via a NoopReporter.
func selectReporter() reporter.Reporter {
	if isQuiet() {
		return reporter.NoopReporter{}
	}
	return clix.NewReporter()
}

// selectDownloadProgress returns the download progress callback to use. In quiet
// mode no progress bar is drawn (nil disables progress reporting in the SDK).
func selectDownloadProgress() download.ProgressFunc {
	if isQuiet() {
		return nil
	}
	return newProgressBar
}
