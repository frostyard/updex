package updex

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/config"
)

// maskFeature turns <dir>/<name>.feature into a systemd mask: a symlink to
// /dev/null, which config parses as a masked, disabled feature.
func maskFeature(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name+".feature")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
	if err := os.Symlink("/dev/null", path); err != nil {
		t.Fatalf("mask %s: %v", path, err)
	}
}

// filterFixture is a domain with two enabled features, one disabled feature
// and one masked feature, each with its own transfer and its own revision
// server publishing 1.0.0 and 2.0.0.
func filterFixture(t *testing.T) *reqFixture {
	t.Helper()
	f := newReqFixture(t)
	f.feature("a", true)
	f.feature("b", true)
	f.feature("off", false)
	f.feature("blocked", true)
	f.transfer("A", []string{"a"}, nil)
	f.transfer("B", []string{"b"}, nil)
	f.transfer("OFF", []string{"off"}, nil)
	f.transfer("BLOCKED", []string{"blocked"}, nil)
	maskFeature(t, f.defDir, "blocked")
	return f
}

// TestSelectFeatures covers the validation the Features option relies on: no
// names mean no filter, and every name that the operation would not act on is
// reported, in argument order and once per distinct name.
func TestSelectFeatures(t *testing.T) {
	features := []*config.Feature{
		{Name: "a", Enabled: true},
		{Name: "b", Enabled: true},
		{Name: "off", Enabled: false},
		{Name: "blocked", Masked: true},
	}

	tests := []struct {
		name     string
		names    []string
		want     []string // selected names, in no particular order; nil for no filter
		wantErrs []string // one entry per reported problem, in argument order
	}{
		{name: "no names is no filter"},
		{name: "one valid name", names: []string{"a"}, want: []string{"a"}},
		{name: "two valid names", names: []string{"b", "a"}, want: []string{"a", "b"}},
		{name: "duplicate name is selected once", names: []string{"a", "a"}, want: []string{"a"}},
		{
			name:     "duplicate unknown name is reported once",
			names:    []string{"nope", "nope"},
			wantErrs: []string{"feature 'nope' not found"},
		},
		{
			name:     "unknown name",
			names:    []string{"nope"},
			wantErrs: []string{"feature 'nope' not found"},
		},
		{
			name:     "empty name is unknown",
			names:    []string{""},
			wantErrs: []string{"feature '' not found"},
		},
		{
			name:     "masked name",
			names:    []string{"blocked"},
			wantErrs: []string{"feature 'blocked' is masked"},
		},
		{
			name:     "disabled name",
			names:    []string{"off"},
			wantErrs: []string{"feature 'off' is not enabled; run 'updex features enable off' first"},
		},
		{
			name:  "several bad names are all reported in argument order",
			names: []string{"nope", "off", "blocked", "a"},
			wantErrs: []string{
				"feature 'nope' not found",
				"feature 'off' is not enabled; run 'updex features enable off' first",
				"feature 'blocked' is masked",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selected, err := selectFeatures(features, tt.names)

			if len(tt.wantErrs) > 0 {
				if err == nil {
					t.Fatalf("selectFeatures(%v) returned no error, want %v", tt.names, tt.wantErrs)
				}
				if got := err.Error(); got != strings.Join(tt.wantErrs, "\n") {
					t.Errorf("selectFeatures(%v) error = %q, want %q", tt.names, got, strings.Join(tt.wantErrs, "\n"))
				}
				if selected != nil {
					t.Errorf("selectFeatures(%v) selected = %v, want nil on error", tt.names, selected)
				}
				return
			}

			if err != nil {
				t.Fatalf("selectFeatures(%v) failed: %v", tt.names, err)
			}
			if tt.want == nil {
				if selected != nil {
					t.Errorf("selectFeatures(%v) selected = %v, want nil (no filter)", tt.names, selected)
				}
				return
			}
			got := make([]string, 0, len(selected))
			for name := range selected {
				got = append(got, name)
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("selectFeatures(%v) selected = %v, want %v", tt.names, got, tt.want)
			}
		})
	}
}

// wanted gets returns the download count of every component in the fixture:
// the ones the table names plus 0 for those it leaves out.
func wantedGets(got map[string]int32) map[string]int32 {
	all := map[string]int32{"A": 0, "B": 0, "OFF": 0, "BLOCKED": 0}
	for component, n := range got {
		all[component] = n
	}
	return all
}

// TestUpdateFeatures_FeatureFilter covers the Features option of
// UpdateFeatures: a run with names acts on those features only, and a name
// that cannot be updated fails the whole run before anything is downloaded,
// linked, vacuumed or refreshed.
func TestUpdateFeatures_FeatureFilter(t *testing.T) {
	tests := []struct {
		name       string
		names      []string
		dryRun     bool
		wantErr    string
		wantResult []string         // features in the result, in domain order
		wantGets   map[string]int32 // component -> downloads
	}{
		{
			name:       "no names updates every enabled feature",
			wantResult: []string{"a", "b"},
			wantGets:   map[string]int32{"A": 1, "B": 1},
		},
		{
			name:       "one named feature",
			names:      []string{"a"},
			wantResult: []string{"a"},
			wantGets:   map[string]int32{"A": 1},
		},
		{
			name:       "duplicate name acts once",
			names:      []string{"a", "a"},
			wantResult: []string{"a"},
			wantGets:   map[string]int32{"A": 1},
		},
		{
			name:       "results keep domain order, not argument order",
			names:      []string{"b", "a"},
			wantResult: []string{"a", "b"},
			wantGets:   map[string]int32{"A": 1, "B": 1},
		},
		{
			name:       "dry run with a filter",
			names:      []string{"b"},
			dryRun:     true,
			wantResult: []string{"b"},
			// A dry run downloads nothing.
		},
		{
			name:    "unknown name fails the whole run",
			names:   []string{"nope"},
			wantErr: "feature 'nope' not found",
		},
		{
			name:    "disabled name fails the whole run",
			names:   []string{"off"},
			wantErr: "feature 'off' is not enabled; run 'updex features enable off' first",
		},
		{
			name:    "masked name fails the whole run",
			names:   []string{"blocked"},
			wantErr: "feature 'blocked' is masked",
		},
		{
			name:    "a valid name does not rescue a bad one",
			names:   []string{"a", "nope"},
			wantErr: "feature 'nope' not found",
		},
		{
			name:    "several bad names are all reported",
			names:   []string{"nope", "off"},
			wantErr: "feature 'nope' not found\nfeature 'off' is not enabled; run 'updex features enable off' first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filterFixture(t)

			results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{
				DryRun:   tt.dryRun,
				Features: tt.names,
			})

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("UpdateFeatures(%v) returned no error, want %q", tt.names, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("UpdateFeatures(%v) error = %q, want it to contain %q", tt.names, err, tt.wantErr)
				}
				if results != nil {
					t.Errorf("UpdateFeatures(%v) returned results %+v, want nil", tt.names, results)
				}
				// Nothing was downloaded, staged, linked or refreshed.
				for component, want := range wantedGets(nil) {
					if got := f.gets(component); got != want {
						t.Errorf("%s downloaded %d times, want %d", component, got, want)
					}
					if f.staged(component, "2.0.0") {
						t.Errorf("%s_2.0.0.raw is staged, want nothing downloaded", component)
					}
					if f.linkExists(component) {
						t.Errorf("%s is linked, want no link", component)
					}
				}
				if f.runner.refreshes != 0 {
					t.Errorf("Refresh called %d times, want 0", f.runner.refreshes)
				}
				// The lock is released again: the next run works.
				if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{
					NoRefresh: true,
					Features:  []string{"a"},
				}); err != nil {
					t.Fatalf("UpdateFeatures after a failed run: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("UpdateFeatures(%v) failed: %v", tt.names, err)
			}
			got := make([]string, len(results))
			for i, r := range results {
				got[i] = r.Feature
			}
			if !slices.Equal(got, tt.wantResult) {
				t.Errorf("UpdateFeatures(%v) features = %v, want %v", tt.names, got, tt.wantResult)
			}
			for component, want := range wantedGets(tt.wantGets) {
				if got := f.gets(component); got != want {
					t.Errorf("%s downloaded %d times, want %d", component, got, want)
				}
				if tt.dryRun {
					if f.staged(component, "2.0.0") || f.linkExists(component) {
						t.Errorf("a dry run staged or linked %s", component)
					}
					continue
				}
				// A downloaded transfer is also linked; one that was left
				// out is neither staged nor linked.
				if linked := f.linked(component) != ""; linked != (want > 0) {
					t.Errorf("%s linked = %v, want %v", component, linked, want > 0)
				}
			}
		})
	}
}

// TestCheckFeatures_FeatureFilter covers the Features option of CheckFeatures:
// a check with names reports those features only, and a name that cannot be
// checked fails the whole run.
func TestCheckFeatures_FeatureFilter(t *testing.T) {
	tests := []struct {
		name       string
		names      []string
		wantErr    string
		wantResult []string
	}{
		{name: "no names checks every enabled feature", wantResult: []string{"a", "b"}},
		{name: "one named feature", names: []string{"a"}, wantResult: []string{"a"}},
		{name: "duplicate name checked once", names: []string{"b", "b"}, wantResult: []string{"b"}},
		{name: "results keep domain order", names: []string{"b", "a"}, wantResult: []string{"a", "b"}},
		{name: "unknown name fails the whole run", names: []string{"nope"}, wantErr: "feature 'nope' not found"},
		{
			name:    "disabled name fails the whole run",
			names:   []string{"off"},
			wantErr: "feature 'off' is not enabled; run 'updex features enable off' first",
		},
		{name: "masked name fails the whole run", names: []string{"blocked"}, wantErr: "feature 'blocked' is masked"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filterFixture(t)

			results, err := f.client.CheckFeatures(t.Context(), CheckFeaturesOptions{Features: tt.names})

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("CheckFeatures(%v) returned no error, want %q", tt.names, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("CheckFeatures(%v) error = %q, want it to contain %q", tt.names, err, tt.wantErr)
				}
				if results != nil {
					t.Errorf("CheckFeatures(%v) returned results %+v, want nil", tt.names, results)
				}
				return
			}

			if err != nil {
				t.Fatalf("CheckFeatures(%v) failed: %v", tt.names, err)
			}
			got := make([]string, len(results))
			for i, r := range results {
				got[i] = r.Feature
			}
			if !slices.Equal(got, tt.wantResult) {
				t.Errorf("CheckFeatures(%v) features = %v, want %v", tt.names, got, tt.wantResult)
			}
			if f.servers["A"].fileGets.Load() != 0 || f.servers["B"].fileGets.Load() != 0 {
				t.Error("check must not download an image")
			}
		})
	}
}
