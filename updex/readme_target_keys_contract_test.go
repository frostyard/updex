package updex

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestReadmeDocumentsTargetKeys pins README.md's "[Target] Section" options
// table to the keys config/transfer.go actually reads via sec.GetKey in its
// [Target] parse block: every parsed key must be named in the table, so a new
// [Target] key cannot land undocumented (as PathRelativeTo and ReadOnly once
// did). It reads source only and changes no parser behavior.
func TestReadmeDocumentsTargetKeys(t *testing.T) {
	parsed := targetKeysParsedBySource(t)
	if len(parsed) == 0 {
		t.Fatal("found no sec.GetKey calls in the [Target] parse block; the test would prove nothing")
	}

	table := readmeTargetTable(t)
	for _, key := range parsed {
		if !strings.Contains(table, "`"+key+"`") {
			t.Errorf("README.md [Target] Section table does not document the parsed key %q", key)
		}
	}
}

// TestReadmeTargetTypeMatchesSysextFiltering pins README.md's [Target] Type
// row to config.IsSysextTransfer's actual behavior: an omitted Type is
// treated as regular-file, and any other non-empty value is silently
// skipped. It must not regress to describing Type as required.
func TestReadmeTargetTypeMatchesSysextFiltering(t *testing.T) {
	table := readmeTargetTable(t)

	typeRow := ""
	for _, line := range strings.Split(table, "\n") {
		if strings.Contains(line, "`Type`") {
			typeRow = line
			break
		}
	}
	if typeRow == "" {
		t.Fatal("README.md [Target] Section table has no `Type` row")
	}

	if strings.Contains(typeRow, "Must be `regular-file`") {
		t.Error("README.md [Target] Type row says Type is required, but an omitted Type is treated as regular-file (see config.IsSysextTransfer)")
	}
	if !strings.Contains(typeRow, "omitted") && !strings.Contains(typeRow, "implicit") {
		t.Error("README.md [Target] Type row does not document that an omitted Type defaults to regular-file")
	}
	if !strings.Contains(typeRow, "skipped") {
		t.Error("README.md [Target] Type row does not document that non-regular-file values are silently skipped")
	}
}

// targetKeysParsedBySource returns every key passed to sec.GetKey inside the
// "// Parse [Target] section" block of config/transfer.go.
func targetKeysParsedBySource(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../config/transfer.go")
	if err != nil {
		t.Fatalf("read config/transfer.go: %v", err)
	}
	source := string(data)

	start := strings.Index(source, "// Parse [Target] section")
	if start == -1 {
		t.Fatal("config/transfer.go has no \"// Parse [Target] section\" marker")
	}
	end := strings.Index(source[start:], "missing [Target] section")
	if end == -1 {
		t.Fatal("config/transfer.go [Target] parse block does not terminate as expected")
	}
	block := source[start : start+end]

	re := regexp.MustCompile(`sec\.GetKey\("([^"]+)"\)`)
	var keys []string
	for _, match := range re.FindAllStringSubmatch(block, -1) {
		keys = append(keys, match[1])
	}
	return keys
}

// readmeTargetTable returns the markdown of README.md's "[Target] Section"
// options table (up to the next heading).
func readmeTargetTable(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	contents := string(data)

	start := strings.Index(contents, "#### [Target] Section")
	if start == -1 {
		t.Fatal("README.md does not contain the \"[Target] Section\" heading")
	}
	rest := contents[start+len("#### [Target] Section"):]
	end := strings.Index(rest, "\n#")
	if end == -1 {
		return rest
	}
	return rest[:end]
}
