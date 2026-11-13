package version

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	goversion "github.com/hashicorp/go-version"
)

// Pattern represents a parsed match pattern with @v and other placeholders
type Pattern struct {
	raw      string
	regex    *regexp.Regexp
	template string

	// versionGroup is the submatch index of the first @v group.
	versionGroup int
	// suiteGroups are the submatch indexes of the SuiteWildcard groups of a
	// pattern from ParseAnySuitePattern; nil for every other pattern.
	suiteGroups []int
}

// SuiteWildcard stands for the suite (os-release VERSION_ID, the %w
// specifier) in a pattern given to ParseAnySuitePattern. It is a NUL byte: a
// Linux file name can never hold one, so no pattern that config text spells
// out can mean it, and ParsePattern keeps treating every other character
// (a literal "@w" included) as it always did. config builds these patterns as
// TargetSection.AnySuitePatterns.
const SuiteWildcard = "\x00"

// suiteRegex matches one VERSION_ID value: 1 or more of 0-9, a-z, '.', '~'
// and '-'. It has no '_': snosi separates the fields of an image name with
// '_', so a '_' would let "foo_@v_%w_%a" match images of a component
// "foo_bar".
const suiteRegex = `[0-9a-z.~-]+`

// anySuiteVersionRegex is the @v group of a ParseAnySuitePattern pattern: the
// @v set of ParsePattern without '_'. With a '_', "foo_@v_<suite>_x86-64.raw"
// would still match "foo_bar_1.0_13_x86-64.raw" (version "bar_1.0"), an image
// of the component "foo_bar". A Debian version never holds a '_'; an image
// whose version does is simply not matched, so a cleanup keeps it.
const anySuiteVersionRegex = `([a-zA-Z0-9.+:~-]+)`

// Placeholder definitions for pattern matching
var placeholders = map[string]string{
	"@v": `([a-zA-Z0-9._+:~-]+)`, // Version - required, captured (includes : for epoch, ~ for debian versions)
	"@u": `[a-fA-F0-9-]+`,        // UUID
	"@f": `[0-9]+`,               // Flags
	"@a": `[01]`,                 // GPT NoAuto flag (0 or 1)
	"@g": `[01]`,                 // GrowFileSystem flag
	"@r": `[01]`,                 // Read-only flag
	"@t": `[0-9]+`,               // Modification time
	"@m": `[0-7]+`,               // File mode
	"@s": `[0-9]+`,               // File size
	"@d": `[0-9]+`,               // Tries done
	"@l": `[0-9]+`,               // Tries left
	"@h": `[a-fA-F0-9]+`,         // SHA256 hash
}

// ParsePattern parses a match pattern string into a Pattern struct
func ParsePattern(pattern string) (*Pattern, error) {
	return parsePattern(pattern, false)
}

// ParseAnySuitePattern parses a pattern in which each SuiteWildcard matches
// any suite (a VERSION_ID value, see suiteRegex). All wildcards of 1 file
// name must match the same value. @v matches what it matches for
// ParsePattern except '_' (see anySuiteVersionRegex). Every other character
// means what it means to ParsePattern. Suite reports the value a file name matched.
func ParseAnySuitePattern(pattern string) (*Pattern, error) {
	return parsePattern(pattern, true)
}

func parsePattern(pattern string, anySuite bool) (*Pattern, error) {
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
		if anySuite && placeholder == "@v" {
			regex = anySuiteVersionRegex
		}
		quotedPlaceholder := regexp.QuoteMeta(placeholder)
		regexStr = strings.ReplaceAll(regexStr, quotedPlaceholder, regex)
	}

	// Each suite wildcard becomes its own named group (s0, s1, ...), so the
	// unnamed groups stay the @v groups whatever their order.
	if anySuite {
		var b strings.Builder
		n := 0
		for i := 0; i < len(regexStr); i++ {
			if regexStr[i] == SuiteWildcard[0] {
				fmt.Fprintf(&b, "(?P<s%d>%s)", n, suiteRegex)
				n++
				continue
			}
			b.WriteByte(regexStr[i])
		}
		regexStr = b.String()
	}

	// Anchor the regex
	regexStr = "^" + regexStr + "$"

	compiled, err := regexp.Compile(regexStr)
	if err != nil {
		return nil, err
	}

	p := &Pattern{
		raw:          pattern,
		regex:        compiled,
		template:     template,
		versionGroup: -1,
	}
	for i, name := range compiled.SubexpNames() {
		switch {
		case i == 0:
		case name == "":
			if p.versionGroup < 0 {
				p.versionGroup = i
			}
		default:
			p.suiteGroups = append(p.suiteGroups, i)
		}
	}
	if p.versionGroup < 0 {
		return nil, ErrMissingVersionPlaceholder
	}
	return p, nil
}

// match returns the version and the suite a file name matched. The suite is
// "" for a pattern without SuiteWildcard.
func (p *Pattern) match(filename string) (ver, suite string, ok bool) {
	matches := p.regex.FindStringSubmatch(filename)
	if matches == nil {
		return "", "", false
	}
	for i, g := range p.suiteGroups {
		if i == 0 {
			suite = matches[g]
		} else if matches[g] != suite {
			return "", "", false
		}
	}
	return matches[p.versionGroup], suite, true
}

// ExtractVersion extracts the version string from a filename using the pattern
func (p *Pattern) ExtractVersion(filename string) (string, bool) {
	v, _, ok := p.match(filename)
	return v, ok
}

// Suite reports the suite (the value of the SuiteWildcard) that filename
// matched. It reports false when filename does not match the pattern or the
// pattern has no SuiteWildcard.
func (p *Pattern) Suite(filename string) (string, bool) {
	if len(p.suiteGroups) == 0 {
		return "", false
	}
	_, suite, ok := p.match(filename)
	return suite, ok
}

// Matches checks if a filename matches the pattern
func (p *Pattern) Matches(filename string) bool {
	_, _, ok := p.match(filename)
	return ok
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

// ParsePatterns parses multiple pattern strings, skipping any that fail to parse.
// It returns the first parse error encountered so callers can surface it when all
// patterns fail.
func ParsePatterns(patternStrs []string) ([]*Pattern, error) {
	patterns := make([]*Pattern, 0, len(patternStrs))
	var firstErr error
	for _, s := range patternStrs {
		p, err := ParsePattern(s)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		patterns = append(patterns, p)
	}
	return patterns, firstErr
}

// ParseAnySuitePatterns is ParsePatterns for ParseAnySuitePattern.
func ParseAnySuitePatterns(patternStrs []string) ([]*Pattern, error) {
	patterns := make([]*Pattern, 0, len(patternStrs))
	var firstErr error
	for _, s := range patternStrs {
		p, err := ParseAnySuitePattern(s)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		patterns = append(patterns, p)
	}
	return patterns, firstErr
}

// ExtractSuiteParsed is ExtractVersionParsed for patterns from
// ParseAnySuitePatterns: it also returns the suite the file name matched.
func ExtractSuiteParsed(filename string, patterns []*Pattern) (version, suite string, ok bool) {
	for _, p := range patterns {
		if v, s, matched := p.match(filename); matched {
			return v, s, true
		}
	}
	return "", "", false
}

// ExtractVersionParsed tries to extract a version from a filename using pre-parsed patterns.
// Returns the version and the matching pattern's raw string, or empty strings if no match.
func ExtractVersionParsed(filename string, patterns []*Pattern) (version string, matchedPattern string, ok bool) {
	for _, p := range patterns {
		if v, matched := p.ExtractVersion(filename); matched {
			return v, p.Raw(), true
		}
	}
	return "", "", false
}

// Compare compares two version strings
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func Compare(v1, v2 string) int {
	// Debian-style versions (epoch with ':' or tilde-revisions like
	// "5+29.4.2-2~debian.13~trixie") cannot be compared by semver: hashicorp
	// go-version treats everything after '+' as build metadata and ignores it,
	// so every such version collapses to the same precedence. Route these to a
	// dpkg-compatible comparator instead.
	if isDebianVersion(v1) || isDebianVersion(v2) {
		return compareDebian(v1, v2)
	}

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
	slices.SortFunc(versions, func(a, b string) int {
		return Compare(b, a) // Reversed for descending order
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

// isDebianVersion reports whether v looks like a Debian/dpkg version string,
// i.e. it carries an epoch (':'), a tilde ('~'), or a '+'. Tilde is never
// valid in SemVer and an explicit epoch is the canonical Debian marker. '+'
// is routed here too: SemVer treats everything after '+' as build metadata
// and ignores it, which collapses dpkg-derived versions like
// "1+7.2-debian13-202607011055" (epoch encoded as '+' in sysext image names,
// where ':' is not filename-safe) to the same precedence — dpkg comparison
// orders them correctly.
func isDebianVersion(v string) bool {
	return strings.ContainsAny(v, "~:+")
}

// compareDebian compares two version strings using Debian/dpkg precedence
// rules: epoch first (numeric), then upstream version, then debian revision,
// each segment compared with the dpkg algorithm where '~' sorts before
// everything (including end-of-string).
func compareDebian(a, b string) int {
	ea, ua, ra := splitDebian(a)
	eb, ub, rb := splitDebian(b)

	if ea != eb {
		if ea < eb {
			return -1
		}
		return 1
	}
	if c := verrevcmp(ua, ub); c != 0 {
		return c
	}
	return verrevcmp(ra, rb)
}

// splitDebian splits a version into epoch, upstream version, and debian
// revision. Format: [epoch:]upstream[-revision]. A missing epoch is 0; a
// missing revision is the empty string.
func splitDebian(v string) (epoch int, upstream, revision string) {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		if e, err := strconv.Atoi(v[:i]); err == nil {
			epoch = e
			v = v[i+1:]
		}
	}
	if i := strings.LastIndexByte(v, '-'); i >= 0 {
		upstream = v[:i]
		revision = v[i+1:]
	} else {
		upstream = v
	}
	return epoch, upstream, revision
}

// verrevcmp is a port of dpkg's version segment comparison. It compares two
// strings in alternating runs of non-digit and digit characters. Non-digit
// runs are compared by dpkgOrder (so '~' sorts first); digit runs are compared
// numerically with leading zeros ignored.
func verrevcmp(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		// Compare the non-digit prefix.
		for (i < len(a) && !isASCIIDigit(a[i])) || (j < len(b) && !isASCIIDigit(b[j])) {
			var ac, bc int
			if i < len(a) {
				ac = dpkgOrder(a[i])
			}
			if j < len(b) {
				bc = dpkgOrder(b[j])
			}
			if ac != bc {
				return sign(ac - bc)
			}
			i++
			j++
		}
		// Skip leading zeros in the digit run.
		for i < len(a) && a[i] == '0' {
			i++
		}
		for j < len(b) && b[j] == '0' {
			j++
		}
		// Compare the digit run: a longer run of digits is the larger number.
		firstDiff := 0
		for i < len(a) && j < len(b) && isASCIIDigit(a[i]) && isASCIIDigit(b[j]) {
			if firstDiff == 0 {
				firstDiff = int(a[i]) - int(b[j])
			}
			i++
			j++
		}
		if i < len(a) && isASCIIDigit(a[i]) {
			return 1
		}
		if j < len(b) && isASCIIDigit(b[j]) {
			return -1
		}
		if firstDiff != 0 {
			return sign(firstDiff)
		}
	}
	return 0
}

// dpkgOrder maps a character to its dpkg sort weight. Digits are handled
// separately (weight 0); letters keep their ASCII value; '~' sorts before
// everything else including end-of-string (weight -1); other characters sort
// after letters.
func dpkgOrder(c byte) int {
	switch {
	case isASCIIDigit(c):
		return 0
	case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		return int(c)
	case c == '~':
		return -1
	default:
		return int(c) + 256
	}
}

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
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
