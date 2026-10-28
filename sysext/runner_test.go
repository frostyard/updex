package sysext

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// fakeRun records each call as "name arg arg" and returns the error the
// caller chose for that call.
func fakeRun(failOn map[string]error, calls *[]string) func(string, ...string) error {
	return func(name string, args ...string) error {
		call := strings.Join(append([]string{name}, args...), " ")
		*calls = append(*calls, call)
		return failOn[call]
	}
}

func TestParseSystemdVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		out    string
		want   int
		wantOK bool
	}{
		{name: "262", out: "systemd 262 (262-1)\n", want: 262, wantOK: true},
		{name: "257 debian", out: "systemd 257 (257.9-1~deb13u1)\n", want: 257, wantOK: true},
		{name: "255 without a build suffix", out: "systemd 255\n", want: 255, wantOK: true},
		{name: "first line only", out: "systemd 262 (262-1)\n+PAM +AUDIT\n", want: 262, wantOK: true},
		{name: "empty", out: "", wantOK: false},
		{name: "garbage", out: "garbage", wantOK: false},
		{name: "no digits", out: "systemd x", wantOK: false},
		{name: "one field", out: "systemd", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseSystemdVersion([]byte(tt.out))
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("parseSystemdVersion(%q) = %d, %v; want %d, %v", tt.out, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestDefaultRunnerRefreshRunsTheADR0019Sequence(t *testing.T) {
	t.Parallel()

	sysusersErr := errors.New("sysusers fake failure")
	tmpfilesErr := errors.New("tmpfiles fake failure")
	refreshErr := errors.New("refresh fake failure")
	versionErr := errors.New("systemd-sysext missing")

	const (
		v262 = "systemd 262 (262-1)\n"
		v257 = "systemd 257 (257.9-1~deb13u1)\n"
		v254 = "systemd 254 (254.10-1)\n"
		v253 = "systemd 253 (253.5-1)\n"
		v252 = "systemd 252 (252.38-1~deb12u1)\n"
	)

	// newest is the sequence for systemd 260 and newer, and for a version
	// updex cannot read.
	newest := []string{
		"systemd-sysext refresh --no-reload",
		"systemd-sysusers",
		"systemd-tmpfiles --create --graceful",
		"systemd-sysext refresh --always-refresh=yes",
	}
	// v255To259 is the sequence for systemd 255 to 259: the last refresh is
	// plain, because --always-refresh does not exist yet.
	v255To259 := []string{
		"systemd-sysext refresh --no-reload",
		"systemd-sysusers",
		"systemd-tmpfiles --create --graceful",
		"systemd-sysext refresh",
	}
	// olderGraceful is the sequence for systemd 254: one plain refresh
	// first, then the accounts and paths. It has no last refresh.
	olderGraceful := []string{
		"systemd-sysext refresh",
		"systemd-sysusers",
		"systemd-tmpfiles --create --graceful",
	}
	// older is the sequence for systemd 253 and older: --graceful does not
	// exist there, so tmpfiles gets --create alone.
	older := []string{
		"systemd-sysext refresh",
		"systemd-sysusers",
		"systemd-tmpfiles --create",
	}

	tests := []struct {
		name string
		// version is the output of systemd-sysext --version.
		version string
		// versionErr makes that command fail.
		versionErr error
		// failOn gives the error each recorded call returns.
		failOn map[string]error
		// wantCalls is the exact sequence the refresh must run.
		wantCalls []string
		// wantErrs are substrings the returned error must hold.
		wantErrs []string
		// wantIs are errors the returned error must wrap.
		wantIs []error
		// wantNotIs are errors the returned error must not wrap.
		wantNotIs []error
		// wantStderr is the exact warning text ("" for none).
		wantStderr string
	}{
		{
			name:      "systemd 262 forces the last refresh",
			version:   v262,
			wantCalls: newest,
		},
		{
			name:      "systemd 257 runs a plain last refresh",
			version:   v257,
			wantCalls: v255To259,
		},
		{
			name:      "systemd 254 runs tmpfiles with --graceful and no last refresh",
			version:   v254,
			wantCalls: olderGraceful,
		},
		{
			name:      "systemd 253 runs tmpfiles without --graceful",
			version:   v253,
			wantCalls: older,
		},
		{
			name:      "systemd 252 refreshes before sysusers and never again",
			version:   v252,
			wantCalls: older,
		},
		{
			name:       "an unreadable version takes the newest form",
			versionErr: versionErr,
			wantCalls:  newest,
		},
		{
			name:      "unparsable version text takes the newest form",
			version:   "garbage\n",
			wantCalls: newest,
		},
		{
			name:      "the first refresh fails and nothing else runs",
			version:   v262,
			failOn:    map[string]error{newest[0]: refreshErr},
			wantCalls: newest[:1],
			wantErrs:  []string{"systemd-sysext refresh failed"},
			wantIs:    []error{refreshErr},
		},
		{
			name:      "a sysusers failure keeps going and marks the activation incomplete",
			version:   v262,
			failOn:    map[string]error{newest[1]: sysusersErr},
			wantCalls: newest,
			wantErrs:  []string{"systemd-sysusers failed after refresh"},
			wantIs:    []error{sysusersErr, ErrActivationIncomplete},
		},
		{
			name:       "a tmpfiles failure is only a warning",
			version:    v262,
			failOn:     map[string]error{newest[2]: tmpfilesErr},
			wantCalls:  newest,
			wantStderr: "warning: systemd-tmpfiles --create failed after refresh: tmpfiles fake failure\n",
		},
		{
			name:      "the last refresh fails and is not an incomplete activation",
			version:   v262,
			failOn:    map[string]error{newest[3]: refreshErr},
			wantCalls: newest,
			wantErrs:  []string{"systemd-sysext refresh failed"},
			wantIs:    []error{refreshErr},
			wantNotIs: []error{ErrActivationIncomplete},
		},
		{
			name:      "a sysusers and a last-refresh failure are both reported",
			version:   v262,
			failOn:    map[string]error{newest[1]: sysusersErr, newest[3]: refreshErr},
			wantCalls: newest,
			wantErrs:  []string{"systemd-sysusers failed after refresh", "systemd-sysext refresh failed"},
			wantIs:    []error{sysusersErr, refreshErr},
			wantNotIs: []error{ErrActivationIncomplete},
		},
		{
			name:      "systemd 252 reports an incomplete activation when sysusers fails",
			version:   v252,
			failOn:    map[string]error{"systemd-sysusers": sysusersErr},
			wantCalls: older,
			wantErrs:  []string{"systemd-sysusers failed after refresh"},
			wantIs:    []error{sysusersErr, ErrActivationIncomplete},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calls []string
			var stderr bytes.Buffer
			r := &DefaultRunner{
				run:    fakeRun(tt.failOn, &calls),
				stderr: &stderr,
				output: func(string, ...string) ([]byte, error) {
					return []byte(tt.version), tt.versionErr
				},
			}

			err := r.Refresh()

			if len(calls) != len(tt.wantCalls) {
				t.Fatalf("ran %d command(s) %q, want %d %q", len(calls), calls, len(tt.wantCalls), tt.wantCalls)
			}
			for i, want := range tt.wantCalls {
				if calls[i] != want {
					t.Errorf("call %d = %q, want %q", i+1, calls[i], want)
				}
			}

			if len(tt.wantErrs) == 0 {
				if err != nil {
					t.Errorf("Refresh() = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Fatalf("Refresh() = nil, want an error with %q", tt.wantErrs)
				}
				for _, want := range tt.wantErrs {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not contain %q", err, want)
					}
				}
			}
			for _, want := range tt.wantIs {
				if !errors.Is(err, want) {
					t.Errorf("error %q does not wrap %v", err, want)
				}
			}
			for _, unwanted := range tt.wantNotIs {
				if errors.Is(err, unwanted) {
					t.Errorf("error %q must not wrap %v", err, unwanted)
				}
			}

			if got := stderr.String(); got != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", got, tt.wantStderr)
			}
		})
	}
}

func TestDefaultRunnerMergeAndUnmerge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		run      func(*DefaultRunner) error
		wantCall string
		wantErr  string
	}{
		{
			name:     "merge",
			run:      (*DefaultRunner).Merge,
			wantCall: "systemd-sysext merge",
			wantErr:  "systemd-sysext merge failed",
		},
		{
			name:     "unmerge",
			run:      (*DefaultRunner).Unmerge,
			wantCall: "systemd-sysext unmerge",
			wantErr:  "systemd-sysext unmerge failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" succeeds", func(t *testing.T) {
			t.Parallel()

			var calls []string
			r := &DefaultRunner{run: fakeRun(nil, &calls)}

			if err := tt.run(r); err != nil {
				t.Fatalf("%s() = %v, want nil", tt.name, err)
			}
			if len(calls) != 1 || calls[0] != tt.wantCall {
				t.Errorf("ran %q, want only %q", calls, tt.wantCall)
			}
		})

		t.Run(tt.name+" failure is wrapped", func(t *testing.T) {
			t.Parallel()

			fakeErr := errors.New("fake failure")
			var calls []string
			r := &DefaultRunner{run: fakeRun(map[string]error{tt.wantCall: fakeErr}, &calls)}

			err := tt.run(r)
			if err == nil {
				t.Fatalf("%s() = nil, want an error", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
			if !errors.Is(err, fakeErr) {
				t.Errorf("error %q does not wrap %v", err, fakeErr)
			}
			if len(calls) != 1 || calls[0] != tt.wantCall {
				t.Errorf("ran %q, want only %q", calls, tt.wantCall)
			}
		})
	}
}
