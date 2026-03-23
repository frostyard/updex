package updex

import (
	"fmt"
	"io"
	"time"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/updex"
	"github.com/schollz/progressbar/v3"
)

// newClient creates a new updex client with the appropriate progress reporter.
func newClient() *updex.Client {
	return updex.NewClient(updex.ClientConfig{
		Definitions:        definitions,
		Verify:             verify,
		Verbose:            clix.Verbose,
		Progress:           clix.NewReporter(),
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
