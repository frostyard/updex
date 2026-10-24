package updex

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/imagecompat"
)

// Tests for the compressed-image half of the minimum image version check
// (ADR-0016): a compressed EROFS image is read with the dump.erofs fallback,
// and a host whose erofs-utils is missing or too old fails closed instead of
// skipping the check. The runner is a fake, so no test runs dump.erofs here.

// compressedSysextImage returns a snosi-shaped image whose superblock says
// that its EROFS filesystem is compressed, so the gate takes the dump.erofs
// path. The fake runner, not go-erofs, supplies the release file.
func compressedSysextImage(t *testing.T, name, minValue string) []byte {
	t.Helper()
	image := sysextImage(t, name, minValue)
	magic := []byte{0xE2, 0xE1, 0xF5, 0xE0} // 0xE0F5E1E2, little-endian
	off := bytes.Index(image, magic)
	if off < 0 || off+86 > len(image) {
		t.Fatal("fixture image has no complete EROFS superblock")
	}
	out := bytes.Clone(image)
	copy(out[off+80:off+84], []byte{0x01, 0x00, 0x00, 0x00}) // LZ4_0PADDING
	copy(out[off+84:off+86], []byte{0xff, 0xff})             // available_compr_algs
	return out
}

// fakeErofsDump is an imagecompat.ErofsDumpRunner that answers for one
// compressed fixture and records what it was asked.
type fakeErofsDump struct {
	version    string
	versionErr error
	// files maps a file name in usr/lib/extension-release.d to its content.
	files map[string]string

	calls      []string
	lastOffset int64
}

func (f *fakeErofsDump) Version(context.Context) (string, error) {
	f.calls = append(f.calls, "version")
	if f.versionErr != nil {
		return "", f.versionErr
	}
	if f.version == "" {
		return "1.8.6", nil
	}
	return f.version, nil
}

func (f *fakeErofsDump) ListDir(_ context.Context, _ string, offset int64, dir string) ([]imagecompat.ErofsDirEntry, error) {
	f.calls = append(f.calls, "list "+dir)
	f.lastOffset = offset
	entries := []imagecompat.ErofsDirEntry{{Name: "."}, {Name: ".."}}
	for name := range f.files {
		entries = append(entries, imagecompat.ErofsDirEntry{Name: name, Regular: true})
	}
	return entries, nil
}

func (f *fakeErofsDump) ReadFile(_ context.Context, _ string, offset int64, file string, maxSize int) ([]byte, error) {
	f.calls = append(f.calls, "read "+file)
	f.lastOffset = offset
	content, ok := f.files[path.Base(file)]
	if !ok {
		return nil, fmt.Errorf("%w: no file %s", imagecompat.ErrUnreadableRelease, file)
	}
	if len(content) > maxSize {
		return nil, fmt.Errorf("%w: %d bytes", imagecompat.ErrReleaseFileTooLarge, len(content))
	}
	return []byte(content), nil
}

// toolMissingErofsDump is what the real runner reports without erofs-utils.
func toolMissingErofsDump() *fakeErofsDump {
	return &fakeErofsDump{versionErr: fmt.Errorf("dump.erofs is not installed: %w", exec.ErrNotFound)}
}

// withErofsRunner replaces the host's client with one that uses runner for the
// dump.erofs fallback.
func (h *minImageHost) withErofsRunner(r *fakeErofsDump) *minImageHost {
	cfg := h.client.config
	cfg.ErofsDumpRunner = r
	h.client = NewClient(cfg)
	return h
}

func TestUpdateFeatures_MinImageVersion_CompressedImage(t *testing.T) {
	tests := []struct {
		name       string
		image      []byte
		runner     *fakeErofsDump
		wantLinked bool
		wantNeeds  string
	}{
		{
			name:  "compressed image with the key too new: not linked",
			image: compressedSysextImage(t, "tool", "20270101000000"),
			runner: &fakeErofsDump{files: map[string]string{
				"extension-release.tool": "SYSEXT_ID=tool\nUPDEX_MIN_IMAGE_VERSION=20270101000000\n",
			}},
			wantNeeds: "needs image >= 20270101000000",
		},
		{
			name:  "compressed image new enough: linked",
			image: compressedSysextImage(t, "tool", "20260101000000"),
			runner: &fakeErofsDump{files: map[string]string{
				"extension-release.tool": "SYSEXT_ID=tool\nUPDEX_MIN_IMAGE_VERSION=20260101000000\n",
			}},
			wantLinked: true,
		},
		{
			name:  "compressed image without the key: linked",
			image: compressedSysextImage(t, "tool", ""),
			runner: &fakeErofsDump{files: map[string]string{
				"extension-release.tool": "SYSEXT_ID=tool\n",
			}},
			wantLinked: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
				minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: tt.image}})
			h.withErofsRunner(tt.runner)
			h.preinstall("tool", oldVersion, true)

			r := mustUpdate(t, h)["tool"]
			if r.NeedsImage != tt.wantNeeds {
				t.Errorf("NeedsImage = %q, want %q", r.NeedsImage, tt.wantNeeds)
			}
			if tt.wantLinked {
				if got := h.linked("tool"); got != newVersion {
					t.Errorf("linked version = %q, want %q", got, newVersion)
				}
			} else if got := h.linked("tool"); got != oldVersion {
				t.Errorf("linked version = %q, want the old %q kept", got, oldVersion)
			}
			// The fallback ran on the staged GPT image: the partition starts
			// at 1 MiB.
			if len(tt.runner.calls) == 0 {
				t.Fatal("the dump.erofs fallback was not used")
			}
			if tt.runner.lastOffset != 1<<20 {
				t.Errorf("fallback offset = %d, want the partition offset %d", tt.runner.lastOffset, 1<<20)
			}
			if !strings.Contains(strings.Join(tt.runner.calls, ","), "extension-release.tool") {
				t.Errorf("fallback calls = %v, want the release file", tt.runner.calls)
			}
		})
	}
}

// Without erofs-utils a compressed image is an error for its feature, nothing
// is linked, and the error names the install. The run fails and another
// feature still updates: the check is never silently skipped.
func TestUpdateFeatures_MinImageVersion_CompressedToolMissing(t *testing.T) {
	h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
		minComp{name: "broken", feature: "brokenfeat", versions: map[string][]byte{newVersion: compressedSysextImage(t, "broken", "")}},
		minComp{name: "plain", feature: "plainfeat", versions: map[string][]byte{newVersion: sysextImage(t, "plain", "")}},
	)
	h.withErofsRunner(toolMissingErofsDump())
	h.preinstall("broken", oldVersion, true)
	h.preinstall("plain", oldVersion, true)

	results, err := h.update(UpdateFeaturesOptions{})
	if err == nil {
		t.Fatal("UpdateFeatures returned no error, want one for the missing tool")
	}
	r := results["broken"]
	if !strings.Contains(r.Error, "erofs-utils >= 1.8.5 is required") || !strings.Contains(r.Error, "install erofs-utils") {
		t.Errorf("broken error = %q, want the install hint", r.Error)
	}
	if h.linked("broken") != oldVersion {
		t.Errorf("broken linked = %q, want the old %q kept", h.linked("broken"), oldVersion)
	}
	if !h.staged("broken", newVersion) {
		t.Error("the downloaded image was removed")
	}
	if p := results["plain"]; p.Error != "" || h.linked("plain") != newVersion {
		t.Errorf("plain = %+v, linked %q; want it linked", p, h.linked("plain"))
	}
	if h.bootc.calls != 0 {
		t.Errorf("bootc ran %d times; the image could not be read", h.bootc.calls)
	}
}

// An uncompressed image never uses the fallback, so a host without
// erofs-utils updates ordinary sysexts as before.
func TestUpdateFeatures_MinImageVersion_UncompressedDoesNotUseTheTool(t *testing.T) {
	h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
		minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: sysextImage(t, "tool", "20260101000000")}})
	runner := toolMissingErofsDump()
	h.withErofsRunner(runner)
	h.preinstall("tool", oldVersion, true)

	r := mustUpdate(t, h)["tool"]
	if r.Error != "" || h.linked("tool") != newVersion {
		t.Fatalf("result = %+v, linked %q; want it linked", r, h.linked("tool"))
	}
	if len(runner.calls) != 0 {
		t.Errorf("the fallback ran for an uncompressed image: %v", runner.calls)
	}
}

// A pending link of another feature that is compressed cannot be checked
// without erofs-utils, so the refresh is skipped and the link stays. The run
// is not an error: the link is right for the next boot.
func TestUpdateFeatures_RefreshGate_CompressedPendingLinkToolMissing(t *testing.T) {
	other := compressedSysextImage(t, "other", "")
	h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
		minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: sysextImage(t, "tool", "")}},
		minComp{name: "other", feature: "otherfeat", versions: map[string][]byte{newVersion: other}})
	h.withErofsRunner(toolMissingErofsDump())
	h.preinstall("tool", oldVersion, true)
	h.preinstallImage("other", newVersion, other, true)
	createFeatureFile(t, filepath.Join(h.root, "sysupdate.d"), "otherfeat", false)

	results, err := h.updateWithRefresh(UpdateFeaturesOptions{NoVacuum: true})
	if err != nil {
		t.Fatalf("UpdateFeatures: %v (a skipped refresh is not an error)", err)
	}
	if h.runner.refreshes != 0 {
		t.Errorf("the refresh ran %d times, want 0", h.runner.refreshes)
	}
	if h.linked("tool") != newVersion || h.linked("other") != newVersion {
		t.Errorf("links: tool=%q other=%q, want both at %s", h.linked("tool"), h.linked("other"), newVersion)
	}
	skipped := results[0].RefreshSkipped
	if !strings.Contains(skipped, "refresh skipped") || !strings.Contains(skipped, "cannot check other") {
		t.Errorf("RefreshSkipped = %q", skipped)
	}
	if !strings.Contains(skipped, "install erofs-utils") {
		t.Errorf("RefreshSkipped = %q, want the install hint", skipped)
	}
}

// A compressed image whose release directory holds no release file is a
// fail-closed error, never ErrUnsupportedImage: the caller must not link it.
func TestUpdateFeatures_MinImageVersion_CompressedNoReleaseFile(t *testing.T) {
	h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
		minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: compressedSysextImage(t, "tool", "")}})
	h.withErofsRunner(&fakeErofsDump{files: map[string]string{"other": "x"}})
	h.preinstall("tool", oldVersion, true)

	results, err := h.update(UpdateFeaturesOptions{})
	if err == nil {
		t.Fatal("UpdateFeatures returned no error, want one")
	}
	if r := results["tool"]; !strings.Contains(r.Error, "no extension-release file") {
		t.Errorf("error = %q, want the failing listing reported", r.Error)
	}
	if h.linked("tool") != oldVersion {
		t.Errorf("linked = %q, want the old %q kept", h.linked("tool"), oldVersion)
	}
	if h.bootc.calls != 0 {
		t.Errorf("bootc ran %d times; the image could not be read", h.bootc.calls)
	}
}

// The status paths (`features list`, `features check`) read a compressed
// staged image through the same fallback and show the same reason.
func TestFeatures_MinImageVersion_CompressedStatus(t *testing.T) {
	h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
		minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: compressedSysextImage(t, "tool", "20270101000000")}})
	runner := &fakeErofsDump{files: map[string]string{
		"extension-release.tool": "SYSEXT_ID=tool\nUPDEX_MIN_IMAGE_VERSION=20270101000000\n",
	}}
	h.withErofsRunner(runner)
	h.preinstall("tool", oldVersion, true)
	writeFile(t, filepath.Join(h.targetDir, "tool_"+newVersion+".raw"), compressedSysextImage(t, "tool", "20270101000000"))

	features, err := h.client.Features(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(features) != 1 || len(features[0].TransferStatus) != 1 {
		t.Fatalf("features = %+v", features)
	}
	if got := features[0].TransferStatus[0].NeedsImage; got != "needs image >= 20270101000000" {
		t.Errorf("NeedsImage = %q", got)
	}
	if len(runner.calls) == 0 {
		t.Error("the fallback was not used for the status")
	}
}
