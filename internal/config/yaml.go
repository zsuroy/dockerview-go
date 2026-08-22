package config

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// yamlMap is a one-level nested scalar map: top-level scalar keys plus a
// single level of 2-space-indented sub-tables (the schema only uses the
// "files" table). The config schema is deliberately flat enough that
// pulling in a YAML dependency would be overkill — the parser below covers
// the whole documented subset (key: value pairs, # comments, quoted
// strings, one nested table) and is unit-tested.
type yamlMap struct {
	values map[string]string
	tables map[string]map[string]string
}

func newYamlMap() yamlMap {
	return yamlMap{
		values: make(map[string]string),
		tables: make(map[string]map[string]string),
	}
}

func (m yamlMap) get(key string) (string, bool) {
	v, ok := m.values[key]
	return v, ok
}

func (m yamlMap) getTable(key, sub string) (string, bool) {
	t, ok := m.tables[key]
	if !ok {
		return "", false
	}
	v, ok := t[sub]
	return v, ok
}

// parseYAML reads the documented subset: comments, blank lines,
// `key: value`, and one level of 2-space indented `subkey: value` under a
// `parent:` line. Duplicate keys are rejected loudly so a typo in config
// can never silently shadow an earlier value.
func parseYAML(r io.Reader) (yamlMap, error) {
	m := newYamlMap()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	table := ""
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		if stripped, keep := stripComment(line); keep {
			line = stripped
		} else {
			continue // full-line comment
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(line, "\t") {
			return yamlMap{}, fmt.Errorf("config.yaml line %d: tabs are not supported", lineNo)
		}
		indent := countLeadingSpaces(line)
		body := strings.TrimSpace(line)
		if indent == 1 || indent > 2 {
			return yamlMap{}, fmt.Errorf("config.yaml line %d: unsupported indentation (%d spaces); use 0 or 2 spaces", lineNo, indent)
		}
		key, val, ok := splitKV(body)
		if !ok {
			return yamlMap{}, fmt.Errorf("config.yaml line %d: expected `key: value`, got %q", lineNo, body)
		}
		if !validKey(key) {
			return yamlMap{}, fmt.Errorf("config.yaml line %d: malformed key %q", lineNo, key)
		}
		if indent == 0 {
			if val == "" {
				// Parent of a sub-table.
				table = key
				if _, exists := m.tables[key]; !exists {
					m.tables[key] = map[string]string{}
				}
				continue
			}
			table = ""
			if _, dup := m.values[key]; dup {
				return yamlMap{}, fmt.Errorf("config.yaml line %d: duplicate top-level key %q", lineNo, key)
			}
			m.values[key] = unquote(val)
			continue
		}
		// indent == 2: must follow a table header.
		if table == "" {
			return yamlMap{}, fmt.Errorf("config.yaml line %d: %q is indented but has no parent key", lineNo, key)
		}
		if val == "" {
			return yamlMap{}, fmt.Errorf("config.yaml line %d: nested empty tables are not supported", lineNo)
		}
		if _, dup := m.tables[table][key]; dup {
			return yamlMap{}, fmt.Errorf("config.yaml line %d: duplicate key %q.%q", lineNo, table, key)
		}
		m.tables[table][key] = unquote(val)
	}
	if err := scanner.Err(); err != nil {
		return yamlMap{}, err
	}
	return m, nil
}

func countLeadingSpaces(s string) int {
	n := 0
	for _, c := range s {
		if c == ' ' {
			n++
		} else {
			break
		}
	}
	return n
}

// stripComment removes a ` # ...` trailer that starts outside of quotes.
// The second return is false when the whole line is a comment.
func stripComment(line string) (string, bool) {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '#' && !inSingle && !inDouble:
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				rest := strings.TrimRight(line[:i], " \t")
				if rest == "" {
					return "", false // full-line comment
				}
				return rest, true // trailing comment
			}
		}
	}
	return line, true
}

func splitKV(s string) (key, val string, ok bool) {
	idx := strings.IndexByte(s, ':')
	if idx <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(s[:idx])
	val = strings.TrimSpace(s[idx+1:])
	return key, val, true
}

func validKey(k string) bool {
	if k == "" {
		return false
	}
	for _, c := range k {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

func unquote(s string) string {
	if len(s) >= 2 {
		if s[0] == '"' && s[len(s)-1] == '"' {
			if v, err := strconv.Unquote(s); err == nil {
				return v
			}
		}
		if s[0] == '\'' && s[len(s)-1] == '\'' {
			return s[1 : len(s)-1]
		}
	}
	return s
}
