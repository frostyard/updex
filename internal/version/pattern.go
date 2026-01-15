package version

import (
	"regexp"
	"sort"
	"strings"

	goversion "github.com/hashicorp/go-version"
)

// Pattern represents a parsed match pattern with @v and other placeholders
type Pattern struct {
	raw      string
	regex    *regexp.Regexp
	template string
}

// Placeholder definitions for pattern matching
var placeholders = map[string]string{
	"@v": `([a-zA-Z0-9._+-]+)`, // Version - required, captured
	"@u": `[a-fA-F0-9-]+`,      // UUID
	"@f": `[0-9]+`,             // Flags
	"@a": `[a-zA-Z0-9_]*`,      // Architecture (amd64, arm64, etc.) - can be empty
	"@g": `[01]`,               // GrowFileSystem flag
	"@r": `[01]`,               // Read-only flag
	"@t": `[0-9]+`,             // Modification time
	"@m": `[0-7]+`,             // File mode
	"@s": `[0-9]+`,             // File size
	"@d": `[0-9]+`,             // Tries done
	"@l": `[0-9]+`,             // Tries left
	"@h": `[a-fA-F0-9]+`,       // SHA256 hash
}

// ParsePattern parses a match pattern string into a Pattern struct
func ParsePattern(pattern string) (*Pattern, error) {
	if pattern == "" {
		return nil, ErrEmptyPattern
	}

	if !strings.Contains(pattern, "@v") {
		return nil, ErrMissingVersionPlaceholder
	}

	// Build regex from pattern
	regexStr := regexp.QuoteMeta(pattern)
	template := pattern

	// Replace placeholders with regex patterns
	for placeholder, regex := range placeholders {
		quotedPlaceholder := regexp.QuoteMeta(placeholder)
		regexStr = strings.ReplaceAll(regexStr, quotedPlaceholder, regex)
	}

	// Anchor the regex
	regexStr = "^" + regexStr + "$"

	compiled, err := regexp.Compile(regexStr)
	if err != nil {
		return nil, err
	}

	return &Pattern{
		raw:      pattern,
		regex:    compiled,
		template: template,
	}, nil
}

// ExtractVersion extracts the version string from a filename using the pattern
func (p *Pattern) ExtractVersion(filename string) (string, bool) {
	matches := p.regex.FindStringSubmatch(filename)
	if len(matches) < 2 {
		return "", false
	}
	// First capture group is the version (@v)
	return matches[1], true
}

// Matches checks if a filename matches the pattern
func (p *Pattern) Matches(filename string) bool {
	return p.regex.MatchString(filename)
}

// BuildFilename builds a filename from the pattern template with the given version
func (p *Pattern) BuildFilename(version string) string {
	result := p.template
	result = strings.ReplaceAll(result, "@v", version)
	// Remove other placeholders (they're optional in output)
	for placeholder := range placeholders {
		if placeholder != "@v" {
			result = strings.ReplaceAll(result, placeholder, "")
		}
	}
	return result
}

// Raw returns the original pattern string
func (p *Pattern) Raw() string {
	return p.raw
}

// ExtractVersionMulti tries to extract a version from a filename using multiple patterns
// Returns the version and the matching pattern, or empty strings if no match
func ExtractVersionMulti(filename string, patternStrs []string) (version string, matchedPattern string, ok bool) {
	for _, patternStr := range patternStrs {
		p, err := ParsePattern(patternStr)
		if err != nil {
			continue
		}
		if v, matched := p.ExtractVersion(filename); matched {
			return v, patternStr, true
		}
	}
	return "", "", false
}

// Compare compares two version strings
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func Compare(v1, v2 string) int {
	ver1, err1 := goversion.NewVersion(normalizeVersion(v1))
	ver2, err2 := goversion.NewVersion(normalizeVersion(v2))

	// If both parse successfully, use semantic comparison
	if err1 == nil && err2 == nil {
		return ver1.Compare(ver2)
	}

	// Fall back to string comparison
	if v1 < v2 {
		return -1
	}
	if v1 > v2 {
		return 1
	}
	return 0
}

// Sort sorts version strings in descending order (newest first)
func Sort(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		return Compare(versions[i], versions[j]) > 0
	})
}

// normalizeVersion attempts to normalize a version string for comparison
func normalizeVersion(v string) string {
	// Remove common prefixes
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")

	// Replace underscores with dots (common in some versioning schemes)
	// But be careful not to break things like "1.0_rc1"
	return v
}

// Errors
var (
	ErrEmptyPattern              = patternError("pattern cannot be empty")
	ErrMissingVersionPlaceholder = patternError("pattern must contain @v placeholder")
)

type patternError string

func (e patternError) Error() string {
	return string(e)
}
