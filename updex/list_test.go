package updex

import (
	"slices"
	"testing"

	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/manifest"
)

// TestGetAvailableVersions_DeterministicOrder verifies that getAvailableVersions
// returns versions in a stable order regardless of map iteration order. Combined
// with the stable version.Sort, this guarantees that a comparator gap (two
// versions comparing equal) resolves reproducibly instead of randomly.
func TestGetAvailableVersions_DeterministicOrder(t *testing.T) {
	client := NewClient(ClientConfig{})
	transfer := &config.Transfer{
		Source: config.SourceSection{
			Type:         "url-file",
			MatchPattern: "testext_@v.raw",
		},
	}
	m := &manifest.Manifest{
		Files: map[string]string{
			"testext_1.0.0.raw": "hash1",
			"testext_1.0.1.raw": "hash2",
			"testext_1.1.0.raw": "hash3",
			"testext_1.2.0.raw": "hash4",
			"testext_2.0.0.raw": "hash5",
			"testext_2.1.0.raw": "hash6",
			"testext_3.0.0.raw": "hash7",
			"testext_3.1.0.raw": "hash8",
		},
	}

	want := []string{"1.0.0", "1.0.1", "1.1.0", "1.2.0", "2.0.0", "2.1.0", "3.0.0", "3.1.0"}

	for range 20 {
		versions, _, _, err := client.getAvailableVersions(t.Context(), transfer, m)
		if err != nil {
			t.Fatalf("getAvailableVersions failed: %v", err)
		}
		if !slices.Equal(versions, want) {
			t.Fatalf("getAvailableVersions returned non-deterministic order: got %v, want %v", versions, want)
		}
	}
}
