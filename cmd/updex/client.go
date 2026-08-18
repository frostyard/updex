package updex

import (
	"fmt"
	"io"
	"os"
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
//
// In JSON or silent mode stdout must carry only machine-readable data, so the
// bar is suppressed entirely by returning nil (download.Download treats a nil
// writer as "no progress"). In text mode the bar and its completion newline are
// written to stderr — matching clix.NewReporter(), which keeps stdout clean for
// data — so a real download under --json no longer corrupts the JSON result.
func newProgressBar(contentLength int64) io.Writer {
	if clix.JSONOutput || clix.Silent {
		return nil
	}
	return progressbar.NewOptions64(
		contentLength,
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionSetDescription("Downloading"),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(40),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() { fmt.Fprintln(os.Stderr) }),
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
