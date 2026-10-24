package updex

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/frostyard/clix"
	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/v2/imagecompat"
	"github.com/frostyard/updex/v2/sysext"
	"github.com/frostyard/updex/v2/systemd"
	"github.com/frostyard/updex/v2/updex"
	"github.com/schollz/progressbar/v3"
)

// sysextRunner is the systemd-sysext runner handed to every CLI-constructed
// client. It stays nil in production so the SDK picks its default runner;
// tests set it to a *sysext.MockRunner to observe refresh/unmerge calls
// without executing systemd-sysext (the same seam pattern as getEUID).
var sysextRunner sysext.SysextRunner

// systemdManager is the systemd manager handed to every CLI-constructed
// client. It stays nil in production so the SDK constructs its default;
// daemon command tests inject a manager rooted at a temporary unit directory.
var systemdManager *systemd.Manager

// runtimePaths is the filesystem path set handed to every CLI-constructed
// client. It stays zero in production so the SDK resolves its usual defaults;
// tests point it at temporary hierarchies through TestMain (main_test.go) or
// setRuntimePaths, so no handler test reads the host's merged-sysext state.
var runtimePaths updex.RuntimePaths

// erofsDumpRunner is the dump.erofs runner handed to every CLI-constructed
// client (the compressed-image fallback of the minimum image version check,
// ADR-0016). It stays nil in production so the SDK runs the real dump.erofs;
// tests set a fake, so no test depends on the host's erofs-utils.
var erofsDumpRunner imagecompat.ErofsDumpRunner

// bootcRunner is the bootc runner handed to every CLI-constructed client (the
// minimum image version check, ADR-0016). It stays nil in production so the
// SDK runs the real bootc; tests set a fake, so no test runs bootc.
var bootcRunner imagecompat.BootcRunner

// newClient creates a new updex client with the appropriate progress reporter.
func newClient() *updex.Client {
	clientConfig := updex.ClientConfig{
		Definitions:     definitions,
		Verify:          verify,
		Verbose:         clix.Verbose,
		Progress:        newReporter(),
		SysextRunner:    sysextRunner,
		BootcRunner:     bootcRunner,
		ErofsDumpRunner: erofsDumpRunner,
		SystemdManager:  systemdManager,
		Paths:           runtimePaths,
	}
	if downloadProgressEnabled() {
		clientConfig.OnDownloadProgress = newProgressBar
	}
	return updex.NewClient(clientConfig)
}

// newReporter returns the progress reporter for the current output mode.
//
// Silent mode discards every event. JSON mode writes the progress and warning
// events as JSON Lines to stderr, so stdout carries exactly one JSON document:
// the command's result. Text mode keeps clix's text reporter, which already
// writes to stderr. clix.NewReporter() must not be used for JSON mode because
// it writes its JSON Lines to stdout.
func newReporter() reporter.Reporter {
	switch {
	case clix.Silent:
		return reporter.NoopReporter{}
	case clix.JSONOutput:
		w := clix.Stderr
		if w == nil {
			w = os.Stderr
		}
		return reporter.NewJSONReporter(w)
	default:
		return clix.NewReporter()
	}
}

// stderrIsTerminal reports whether standard error is a terminal. It is a
// variable so a test can choose the answer without a real terminal.
var stderrIsTerminal = func() bool {
	info, err := os.Stderr.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// downloadProgressEnabled reports whether downloads get a progress bar: only
// in text mode, and only when stderr is a terminal. Under systemd (or any
// non-terminal stderr) the bar's carriage-return redraws would reach the
// journal as raw text, so updex suppresses the bar there.
func downloadProgressEnabled() bool {
	if clix.JSONOutput || clix.Silent {
		return false
	}
	return stderrIsTerminal()
}

// newProgressBar creates a terminal progress bar for download tracking.
//
// In JSON or silent mode stdout must carry only machine-readable data, so the
// bar is suppressed entirely by returning nil (download.Download treats a nil
// writer as "no progress"). In text mode the bar and its completion newline are
// written to stderr — matching the text reporter, which keeps stdout clean for
// data — so a real download under --json no longer corrupts the JSON result.
// newClient only installs this writer when downloadProgressEnabled reports a
// text mode on a terminal; the checks here keep a direct caller safe too.
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
		progressbar.OptionSetWriter(os.Stderr),
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
