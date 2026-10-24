package claude

import "strings"

// Frontmatter returns the top-level scalar keys of a leading ---/--- block and the text after it.
// ponytail: single-line values only (no YAML folding); add a parser if descriptions need it.
func Frontmatter(b []byte) (map[string]string, string) {
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	m := map[string]string{}
	if strings.TrimSpace(lines[0]) != "---" {
		return m, strings.Join(lines, "\n")
	}
	for i, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			return m, strings.Join(lines[i+2:], "\n")
		}
		if k, v, ok := strings.Cut(l, ":"); ok && !strings.HasPrefix(l, " ") {
			v = strings.TrimSpace(v)
			if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
				v = v[1 : len(v)-1]
			}
			m[strings.TrimSpace(k)] = v
		}
	}
	return m, ""
}
