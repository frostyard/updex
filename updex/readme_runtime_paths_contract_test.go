package updex

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestReadmeDocumentsRuntimePathsFields pins README.md's RuntimePaths struct
// block to the live updex.RuntimePaths type: every exported field must be
// named in the README so a new RuntimePaths field cannot land undocumented
// again.
func TestReadmeDocumentsRuntimePathsFields(t *testing.T) {
	typ := reflect.TypeOf(RuntimePaths{})
	if typ.NumField() == 0 {
		t.Fatal("RuntimePaths has no fields; the test would prove nothing")
	}

	data, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	contents := string(data)

	start := strings.Index(contents, "type RuntimePaths struct {")
	if start == -1 {
		t.Fatal("README.md does not contain the RuntimePaths struct block")
	}
	rest := contents[start:]
	end := strings.Index(rest, "}\n```")
	if end == -1 {
		t.Fatal("README.md RuntimePaths struct block does not terminate before the end of the code block")
	}
	block := rest[:end]

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		if !strings.Contains(block, field.Name) {
			t.Errorf("README.md RuntimePaths block does not mention field %q", field.Name)
		}
	}
}
