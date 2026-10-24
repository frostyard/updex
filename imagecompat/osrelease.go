package imagecompat

import (
	"bufio"
	"bytes"
	"strings"
)

// ParseOSRelease parses os-release style data (also used by
// extension-release files): KEY=VALUE lines, with optional single or double
// quotes around the value, backslash escapes in double-quoted and unquoted
// values, and "#" or ";" comment lines. A later assignment replaces an
// earlier one. Lines that are not a valid assignment are ignored. It never
// fails; the caller bounds the size of data.
func ParseOSRelease(data []byte) map[string]string {
	out := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 4096), MaxReleaseFileSize+1)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !validKey(key) {
			continue
		}
		out[key] = parseValue(value)
	}
	return out
}

func validKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// parseValue decodes one value. A quoted value ends at its closing quote;
// anything after it is dropped. An unterminated quote keeps the rest of the
// line.
func parseValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if v[0] == '\'' {
		rest := v[1:]
		if end := strings.IndexByte(rest, '\''); end >= 0 {
			return rest[:end]
		}
		return rest
	}
	quoted := v[0] == '"'
	if quoted {
		v = v[1:]
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '\\' && i+1 < len(v):
			i++
			b.WriteByte(v[i])
		case quoted && c == '"':
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
