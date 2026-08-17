package updex

import (
	"fmt"
	"io"
	"time"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/sysext"
	"github.com/frostyard/updex/updex"
	"github.com/schollz/progressbar/v3"
)

// sysextRunner is the systemd-sysext runner handed to every CLI-constructed
// client. It stays nil in production so the SDK picks its default runner;
// tests set it to a *sysext.MockRunner to observe refresh/unmerge calls
// without executing systemd-sysext (the same seam pattern as getEUID).
var sysextRunner sysext.SysextRunner

// newClient creates a new updex client with the appropriate progress reporter.
func newClient() *updex.Client {
	return updex.NewClient(updex.ClientConfig{
		Definitions:        definitions,
		Verify:             verify,
		Verbose:            clix.Verbose,
		Progress:           clix.NewReporter(),
		SysextRunner:       sysextRunner,
		OnDownloadProgress: newProgressBar,
	})
}

// newProgressBar creates a terminal progress bar for download tracking.
func newProgressBar(contentLength int64) io.Writer {
	return progressbar.NewOptions64(
		contentLength,
		progressbar.OptionSetDescription("Downloading"),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(40),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() { fmt.Println() }),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
}
