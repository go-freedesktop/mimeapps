// Copyright (c) the go-freedesktop/mimeapps authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mimeapps

import (
	"os"
	"path/filepath"
	"strings"
)

// The three group names defined by the specification.
const (
	groupDefault = "Default Applications"
	groupAdded   = "Added Associations"
	groupRemoved = "Removed Associations"
)

// listFile is a parsed mimeapps.list preserving group order, key order and
// any comment / blank lines, so a targeted edit round-trips losslessly.
type listFile struct {
	path   string
	groups []*listGroup
}

// listGroup is a single [Section] and its lines, in file order.
type listGroup struct {
	name  string
	lines []*listLine
}

// listLine is one physical line inside a group. A key/value line has key set
// (values are the ';'-separated desktop-file ids); a comment or blank line
// has key empty and its verbatim text in raw.
type listLine struct {
	key    string
	values []string
	raw    string
}

// parseList parses mimeapps.list content. The format is INI-like and this
// parser is deliberately lenient: it never fails, so a stray line in a
// system file cannot break resolution. Lines before the first group header
// are attached to an anonymous leading group with an empty name.
func parseList(data []byte, path string) *listFile {
	lf := &listFile{path: path}
	var cur *listGroup
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"):
			cur = &listGroup{name: trimmed[1 : len(trimmed)-1]}
			lf.groups = append(lf.groups, cur)
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			cur = lf.ensurePreamble(cur)
			cur.lines = append(cur.lines, &listLine{raw: line})
		default:
			eq := strings.IndexByte(line, '=')
			if eq < 0 {
				// Not a header, comment or key=value: keep verbatim.
				cur = lf.ensurePreamble(cur)
				cur.lines = append(cur.lines, &listLine{raw: line})
				continue
			}
			cur = lf.ensurePreamble(cur)
			key := strings.TrimSpace(line[:eq])
			cur.lines = append(cur.lines, &listLine{
				key:    key,
				values: splitIDs(line[eq+1:]),
			})
		}
	}
	return lf
}

// ensurePreamble returns cur, creating an anonymous leading group the first
// time a non-header line appears before any [Section].
func (lf *listFile) ensurePreamble(cur *listGroup) *listGroup {
	if cur != nil {
		return cur
	}
	g := &listGroup{name: ""}
	lf.groups = append(lf.groups, g)
	return g
}

// splitIDs splits a ';'-separated desktop-id list, trimming spaces and
// dropping empty fields (so a trailing ';' is harmless).
func splitIDs(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ";") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// get returns the desktop-id list for key in the named group, or nil.
func (lf *listFile) get(group, key string) []string {
	for _, g := range lf.groups {
		if g.name != group {
			continue
		}
		for _, ln := range g.lines {
			if ln.key == key {
				return ln.values
			}
		}
	}
	return nil
}

// set replaces (or creates) the value list for key in group, appending a new
// group and/or key at the end when absent so existing order is preserved.
func (lf *listFile) set(group, key string, values []string) {
	g := lf.group(group)
	for _, ln := range g.lines {
		if ln.key == key {
			ln.values = values
			return
		}
	}
	g.lines = append(g.lines, &listLine{key: key, values: values})
}

// group returns the named group, creating and appending it when missing.
func (lf *listFile) group(name string) *listGroup {
	for _, g := range lf.groups {
		if g.name == name {
			return g
		}
	}
	g := &listGroup{name: name}
	lf.groups = append(lf.groups, g)
	return g
}

// bytes serialises the listFile back to mimeapps.list text. Value lists are
// written with a trailing ';' as is conventional for the format.
func (lf *listFile) bytes() []byte {
	var b strings.Builder
	for gi, g := range lf.groups {
		if g.name != "" {
			b.WriteString("[")
			b.WriteString(g.name)
			b.WriteString("]\n")
		}
		for _, ln := range g.lines {
			if ln.key == "" {
				b.WriteString(ln.raw)
				b.WriteString("\n")
				continue
			}
			b.WriteString(ln.key)
			b.WriteString("=")
			b.WriteString(strings.Join(ln.values, ";"))
			b.WriteString(";\n")
		}
		// Blank line between groups (but not after the last).
		if gi < len(lf.groups)-1 && g.name != "" {
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

// save writes the listFile to its path, creating the parent directory.
func (lf *listFile) save() error {
	if err := os.MkdirAll(filepath.Dir(lf.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(lf.path, lf.bytes(), 0o644)
}

// addValue appends id to key in group if not already present.
func (lf *listFile) addValue(group, key, id string) {
	vals := lf.get(group, key)
	for _, v := range vals {
		if v == id {
			return
		}
	}
	lf.set(group, key, append(append([]string{}, vals...), id))
}

// dropValue removes id from key in group, leaving the (possibly empty) list.
func (lf *listFile) dropValue(group, key, id string) {
	vals := lf.get(group, key)
	if vals == nil {
		return
	}
	out := vals[:0:0]
	for _, v := range vals {
		if v != id {
			out = append(out, v)
		}
	}
	lf.set(group, key, out)
}
