package updex

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestSDKAPIDocumentsDownloadOptions pins docs/specs/sdk-api.md to
// download.go's exported Option constructors, error sentinels, and default
// value constants, so a new With*/Err*/Default* identifier cannot land
// undocumented (as DefaultMaxDownloadSize, WithMaxDownloadSize, and
// ErrDownloadTooLarge once did). It reads source only and changes no
// download.go behavior.
func TestSDKAPIDocumentsDownloadOptions(t *testing.T) {
	identifiers := downloadOptionIdentifiers(t)
	if len(identifiers) == 0 {
		t.Fatal("found no exported With*/Err*/Default* identifiers in download/download.go; the test would prove nothing")
	}

	data, err := os.ReadFile("../docs/specs/sdk-api.md")
	if err != nil {
		t.Fatalf("read docs/specs/sdk-api.md: %v", err)
	}
	contents := string(data)

	for _, id := range identifiers {
		if !strings.Contains(contents, id) {
			t.Errorf("docs/specs/sdk-api.md does not mention %q", id)
		}
	}
}

// downloadOptionIdentifiers returns every exported `With*` Option
// constructor, `Err*` error sentinel, and `Default*` default-value constant
// declared at package level in download/download.go.
func downloadOptionIdentifiers(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../download/download.go")
	if err != nil {
		t.Fatalf("read download/download.go: %v", err)
	}
	source := string(data)

	re := regexp.MustCompile(`(?m)^(?:func (With\w+)\(|var (Err\w+)|const (Default\w+))`)
	var identifiers []string
	for _, match := range re.FindAllStringSubmatch(source, -1) {
		for _, group := range match[1:] {
			if group != "" {
				identifiers = append(identifiers, group)
			}
		}
	}
	return identifiers
}
