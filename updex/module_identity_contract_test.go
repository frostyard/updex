package updex

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type modulePathClass uint8

const (
	unrelatedModulePath modulePathClass = iota
	staleModulePath
	canonicalModulePath
)

func TestModuleIdentityAndSelfImports(t *testing.T) {
	const canonicalModule = "github.com/frostyard/updex/v2"

	root := ".."
	moduleData, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	requireModuleDirective(t, string(moduleData), canonicalModule)

	selfModule := strings.TrimSuffix(canonicalModule, "/v2")
	canonicalImports := 0
	sentinelSeen := false
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}

		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("make %q relative to repository root: %w", path, err)
		}
		relativePath = filepath.ToSlash(relativePath)

		source, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", relativePath, err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", relativePath, err)
		}

		for _, imported := range parsed.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return fmt.Errorf("decode import in %s: %w", relativePath, err)
			}
			switch classifyModulePath(value, selfModule, canonicalModule) {
			case staleModulePath:
				t.Errorf("%s: stale self-module import %q", relativePath, value)
			case canonicalModulePath:
				canonicalImports++
			}
		}

		var inspectErr error
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING || inspectErr != nil {
				return true
			}

			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				inspectErr = fmt.Errorf("decode string literal in %s: %w", relativePath, err)
				return false
			}
			class := classifyModulePath(value, selfModule, canonicalModule)
			if class == unrelatedModulePath {
				if trimmed := strings.Trim(value, "\""); trimmed != value {
					class = classifyModulePath(trimmed, selfModule, canonicalModule)
				}
			}
			if class == staleModulePath {
				t.Errorf("%s: stale self-module string literal %q", relativePath, value)
			}

			if relativePath == "cmd/updex/daemon_test.go" &&
				value == strconv.Quote(canonicalModule+"/systemd") {
				sentinelSeen = true
			}
			return true
		})
		if inspectErr != nil {
			return inspectErr
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan repository Go files: %v", err)
	}
	if canonicalImports == 0 {
		t.Error("no canonical self-module imports found")
	}
	if !sentinelSeen {
		t.Errorf(
			"cmd/updex/daemon_test.go does not contain canonical quote-wrapped sentinel %q",
			strconv.Quote(canonicalModule+"/systemd"),
		)
	}
}

func requireModuleDirective(t *testing.T, contents, canonicalModule string) {
	t.Helper()

	want := "module " + canonicalModule
	for line := range strings.SplitSeq(contents, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "module ") {
			if line != want {
				t.Errorf("go.mod module directive = %q, want %q", line, want)
			}
			return
		}
	}
	t.Error("go.mod has no module directive")
}

func classifyModulePath(value, selfModule, canonicalModule string) modulePathClass {
	if value == canonicalModule || strings.HasPrefix(value, canonicalModule+"/") {
		return canonicalModulePath
	}
	if value == selfModule || strings.HasPrefix(value, selfModule+"/") {
		return staleModulePath
	}
	return unrelatedModulePath
}
