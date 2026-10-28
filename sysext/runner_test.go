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

func TestDefaultRunnerRefreshRunsTheADR0019Sequence(t *testing.T) {
	t.Parallel()

	sysusersErr := errors.New("sysusers fake failure")
	tmpfilesErr := errors.New("tmpfiles fake failure")
	refreshErr := errors.New("refresh fake failure")

	all := []string{
		"systemd-sysext refresh --no-reload",
		"systemd-sysusers",
		"systemd-tmpfiles --create --graceful",
		"systemd-sysext refresh",
	}

	tests := []struct {
		name string
		// failOn gives the error each recorded call returns.
		failOn map[string]error
		// wantCalls is the exact sequence the refresh must run.
		wantCalls []string
		// wantErrs are substrings the returned error must hold.
		wantErrs []string
		// wantIs are errors the returned error must wrap.
		wantIs []error
		// wantStderr is the exact warning text ("" for none).
		wantStderr string
	}{
		{
			name:      "all steps succeed",
			wantCalls: all,
		},
		{
			name:      "the first refresh fails and nothing else runs",
			failOn:    map[string]error{all[0]: refreshErr},
			wantCalls: all[:1],
			wantErrs:  []string{"systemd-sysext refresh failed"},
			wantIs:    []error{refreshErr},
		},
		{
			name:      "a sysusers failure keeps going and is an error",
			failOn:    map[string]error{all[1]: sysusersErr},
			wantCalls: all,
			wantErrs:  []string{"systemd-sysusers failed after refresh"},
			wantIs:    []error{sysusersErr},
		},
		{
			name:       "a tmpfiles failure is only a warning",
			failOn:     map[string]error{all[2]: tmpfilesErr},
			wantCalls:  all,
			wantStderr: "warning: systemd-tmpfiles --create failed after refresh: tmpfiles fake failure\n",
		},
		{
			name:      "the second refresh fails",
			failOn:    map[string]error{all[3]: refreshErr},
			wantCalls: all,
			wantErrs:  []string{"systemd-sysext refresh failed"},
			wantIs:    []error{refreshErr},
		},
		{
			name:      "a sysusers and a second-refresh failure are both reported",
			failOn:    map[string]error{all[1]: sysusersErr, all[3]: refreshErr},
			wantCalls: all,
			wantErrs:  []string{"systemd-sysusers failed after refresh", "systemd-sysext refresh failed"},
			wantIs:    []error{sysusersErr, refreshErr},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calls []string
			var stderr bytes.Buffer
			r := &DefaultRunner{run: fakeRun(tt.failOn, &calls), stderr: &stderr}

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
