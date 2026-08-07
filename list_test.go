// Copyright (c) the go-freedesktop/mimeapps authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mimeapps

import (
	"testing"
)

// TestParseRoundTrip parses a file exercising every line kind — a leading
// comment (preamble), a blank line, a section header, key=value lines and a
// stray no-'=' line — and checks the serialisation round-trips.
func TestParseRoundTrip(t *testing.T) {
	src := "# a comment\nstray line without equals\n\n" +
		"[Default Applications]\n" +
		"text/plain=a.desktop;b.desktop;\n\n" +
		"[Added Associations]\n" +
		"image/png=c.desktop;\n"

	lf := parseList([]byte(src), "mem")

	// Values parsed correctly.
	if got := lf.get(groupDefault, "text/plain"); !eq(got, []string{"a.desktop", "b.desktop"}) {
		t.Errorf("parsed default = %v", got)
	}
	// A missing key / group returns nil.
	if got := lf.get(groupRemoved, "text/plain"); got != nil {
		t.Errorf("missing group get = %v, want nil", got)
	}
	if got := lf.get(groupDefault, "no/such"); got != nil {
		t.Errorf("missing key get = %v, want nil", got)
	}

	// Round-trip: re-parsing the serialisation yields the same values.
	lf2 := parseList(lf.bytes(), "mem")
	if got := lf2.get(groupDefault, "text/plain"); !eq(got, []string{"a.desktop", "b.desktop"}) {
		t.Errorf("round-trip default = %v", got)
	}
	if got := lf2.get(groupAdded, "image/png"); !eq(got, []string{"c.desktop"}) {
		t.Errorf("round-trip added = %v", got)
	}
}

// TestSetGroupAndKeyCreation covers set on an existing key, a new key in an
// existing group, and a brand-new group.
func TestSetGroupAndKeyCreation(t *testing.T) {
	lf := parseList([]byte("[Default Applications]\ntext/plain=a.desktop;\n"), "mem")

	// Existing key replaced.
	lf.set(groupDefault, "text/plain", []string{"z.desktop"})
	if got := lf.get(groupDefault, "text/plain"); !eq(got, []string{"z.desktop"}) {
		t.Errorf("replace = %v", got)
	}
	// New key in existing group.
	lf.set(groupDefault, "image/png", []string{"v.desktop"})
	if got := lf.get(groupDefault, "image/png"); !eq(got, []string{"v.desktop"}) {
		t.Errorf("new key = %v", got)
	}
	// New group entirely.
	lf.set(groupRemoved, "text/html", []string{"x.desktop"})
	if got := lf.get(groupRemoved, "text/html"); !eq(got, []string{"x.desktop"}) {
		t.Errorf("new group = %v", got)
	}
}

// TestAddDropValue covers addValue (append + already-present) and dropValue
// (filtering + absent-key no-op).
func TestAddDropValue(t *testing.T) {
	lf := &listFile{path: "mem"}

	lf.addValue(groupAdded, "text/plain", "a.desktop")
	lf.addValue(groupAdded, "text/plain", "a.desktop") // duplicate: no-op
	lf.addValue(groupAdded, "text/plain", "b.desktop")
	if got := lf.get(groupAdded, "text/plain"); !eq(got, []string{"a.desktop", "b.desktop"}) {
		t.Errorf("addValue = %v", got)
	}

	lf.dropValue(groupAdded, "text/plain", "a.desktop")
	if got := lf.get(groupAdded, "text/plain"); !eq(got, []string{"b.desktop"}) {
		t.Errorf("dropValue = %v", got)
	}
	// Dropping from an absent key is a no-op.
	lf.dropValue(groupAdded, "no/such", "b.desktop")
}

// TestReversed checks the small helper without mutating its input.
func TestReversed(t *testing.T) {
	in := []string{"a", "b", "c"}
	if got := reversed(in); !eq(got, []string{"c", "b", "a"}) {
		t.Errorf("reversed = %v", got)
	}
	if in[0] != "a" {
		t.Error("reversed mutated its input")
	}
}
