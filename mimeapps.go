// Copyright (c) the go-freedesktop/mimeapps authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package mimeapps implements the freedesktop.org Association between MIME
// types and applications specification: it reads the mimeapps.list files,
// applies the [Default Applications] / [Added Associations] /
// [Removed Associations] groups with the correct cross-directory precedence,
// and answers the two questions a file manager or an "Open With" menu asks —
// the default application for a MIME type and the ordered list of candidate
// applications.
//
// It does not reinvent the .desktop parser: application discovery, the
// showable filter and the legacy MimeType= associations all come from
// github.com/go-freedesktop/desktopentry, and the XDG base directories from
// github.com/adrg/xdg. On top of those this package adds the association
// layer:
//
//   - the full mimeapps.list search order ($XDG_CONFIG_HOME, $XDG_CONFIG_DIRS,
//     then the applications/ subdirectory of $XDG_DATA_HOME and $XDG_DATA_DIRS),
//     including the higher-priority <desktop>-mimeapps.list variants named
//     after each entry of $XDG_CURRENT_DESKTOP;
//   - [Resolver.DefaultApp], the default application resolved per the spec
//     algorithm (first existing, showable default across the files in priority
//     order, falling back to the first candidate);
//   - [Resolver.Candidates], the ordered candidate list (defaults, then added
//     associations, then the legacy MimeType= keys, minus removed associations);
//   - [Resolver.SetDefault], [Resolver.AddAssociation] and
//     [Resolver.RemoveAssociation], round-trippable edits to the user's
//     mimeapps.list that preserve its groups, key order and comments.
package mimeapps

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adrg/xdg"
	"github.com/go-freedesktop/desktopentry"
)

// Errors returned by this package.
var (
	// ErrNoDefault is returned by [Resolver.DefaultApp] when no default and
	// no candidate application handles the MIME type.
	ErrNoDefault = errors.New("mimeapps: no application associated with MIME type")

	// ErrNotFound is returned by [Resolver.SetDefault] and
	// [Resolver.AddAssociation] when the given desktop-file id does not
	// resolve to an installed, showable application.
	ErrNotFound = errors.New("mimeapps: desktop-file id not found")
)

// Resolver answers association queries against a fixed set of parsed
// mimeapps.list files and a fixed index of installed applications. Build one
// with [Load] (standard XDG environment) or [LoadDirs] (injectable, for
// tests). It is not safe for concurrent mutation via the writer methods.
type Resolver struct {
	// files are the parsed mimeapps.list files in decreasing priority
	// (index 0 is the most important).
	files []*listFile
	// user is the config-home mimeapps.list; the writer methods mutate and
	// persist it. It is a high-priority element of files (only the
	// config-home <desktop>-mimeapps.list variants outrank it).
	user *listFile
	// entries maps a full desktop-file id ("firefox.desktop") to the
	// installed, showable [desktopentry.Entry] it refers to.
	entries map[string]*desktopentry.Entry
}

// Load builds a Resolver from the current process environment: the standard
// XDG configuration and data directories and $XDG_CURRENT_DESKTOP.
func Load() *Resolver {
	return LoadDirs(
		configDirs(),
		appDirs(),
		currentDesktops(),
	)
}

// LoadDirs is the directory-injectable form of [Load].
//
// configDirs are the directories that hold a top-level mimeapps.list, most
// important first (conventionally $XDG_CONFIG_HOME then $XDG_CONFIG_DIRS).
// appDirs are the applications/ directories that hold both the installed
// *.desktop files and the data-level mimeapps.list, most important first
// (conventionally $XDG_DATA_HOME/applications then each
// $XDG_DATA_DIRS/applications). desktops is the $XDG_CURRENT_DESKTOP list; it
// selects the higher-priority <desktop>-mimeapps.list variants and gates the
// showability of candidate applications via OnlyShowIn/NotShowIn.
//
// The user-writable mimeapps.list targeted by the writer methods is
// configDirs[0]/mimeapps.list; it is always represented (created in memory
// when the file is absent) so an edit on a fresh profile succeeds.
func LoadDirs(configDirs, appDirs, desktops []string) *Resolver {
	r := &Resolver{entries: map[string]*desktopentry.Entry{}}

	// Build the ordered list of mimeapps.list search paths: config dirs
	// first, then the applications/ data dirs; within each directory the
	// <desktop>-mimeapps.list variants outrank the plain mimeapps.list.
	var searchDirs []string
	searchDirs = append(searchDirs, configDirs...)
	searchDirs = append(searchDirs, appDirs...)

	userPath := ""
	if len(configDirs) > 0 {
		userPath = filepath.Join(configDirs[0], "mimeapps.list")
	}

	for _, dir := range searchDirs {
		for _, d := range desktops {
			// The <desktop>-mimeapps.list variants use the lowercased
			// desktop name (gnome-mimeapps.list for XDG_CURRENT_DESKTOP=GNOME).
			r.appendFile(filepath.Join(dir, strings.ToLower(d)+"-mimeapps.list"))
		}
		p := filepath.Join(dir, "mimeapps.list")
		if p == userPath {
			// Always represent the user file, even when missing.
			r.user = r.loadOrEmpty(p)
			r.files = append(r.files, r.user)
			continue
		}
		r.appendFile(p)
	}

	// Build the installed-application index from the applications/ dirs.
	// desktopentry.ScanDirs takes dirs in increasing precedence, so reverse
	// our most-important-first appDirs.
	for _, e := range desktopentry.ScanDirs(reversed(appDirs)) {
		if !showableIn(e, desktops) {
			continue
		}
		r.entries[e.ID+".desktop"] = e
	}
	return r
}

// appendFile parses the mimeapps.list at path and appends it to files when it
// exists; unreadable or missing files are skipped.
func (r *Resolver) appendFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	r.files = append(r.files, parseList(data, path))
}

// loadOrEmpty parses the mimeapps.list at path, or returns an empty listFile
// bound to path when it does not exist yet.
func (r *Resolver) loadOrEmpty(path string) *listFile {
	data, err := os.ReadFile(path)
	if err != nil {
		return &listFile{path: path}
	}
	return parseList(data, path)
}

// lookup resolves a full desktop-file id to its installed, showable entry, or
// nil when it is not present.
func (r *Resolver) lookup(id string) *desktopentry.Entry {
	return r.entries[id]
}

// DefaultApp returns the default application for mimeType. It scans the
// [Default Applications] group of each mimeapps.list in priority order and
// returns the first entry that is installed and showable; when no default
// resolves it falls back to the first entry of [Resolver.Candidates].
// It returns [ErrNoDefault] when nothing handles the type.
func (r *Resolver) DefaultApp(mimeType string) (*desktopentry.Entry, error) {
	for _, lf := range r.files {
		for _, id := range lf.get(groupDefault, mimeType) {
			if e := r.lookup(id); e != nil {
				return e, nil
			}
		}
	}
	if c := r.Candidates(mimeType); len(c) > 0 {
		return c[0], nil
	}
	return nil, ErrNoDefault
}

// Candidates returns the ordered list of applications that can open mimeType:
// the defaults first, then the added associations, then the legacy MimeType=
// associations of scanned .desktop entries, with the removed associations
// subtracted and each application listed once. Only installed, showable
// applications are returned.
func (r *Resolver) Candidates(mimeType string) []*desktopentry.Entry {
	var out []*desktopentry.Entry
	for _, id := range r.orderedIDs(mimeType) {
		if e := r.lookup(id); e != nil {
			out = append(out, e)
		}
	}
	return out
}

// orderedIDs computes the association id list for mimeType in candidate
// order, applying the cross-file precedence rules: a Default/Added in a
// higher-priority file is committed and cannot be removed by a
// lower-priority Removed, while a Removed commits a blacklist that later
// files' Added entries cannot override.
func (r *Resolver) orderedIDs(mimeType string) []string {
	var defaults, added []string
	dseen := map[string]bool{}
	aseen := map[string]bool{}
	removed := map[string]bool{}
	committed := map[string]bool{}

	for _, lf := range r.files {
		for _, id := range lf.get(groupDefault, mimeType) {
			if !dseen[id] {
				dseen[id] = true
				defaults = append(defaults, id)
			}
			committed[id] = true
		}
		for _, id := range lf.get(groupAdded, mimeType) {
			if !aseen[id] && !removed[id] {
				aseen[id] = true
				added = append(added, id)
			}
			committed[id] = true
		}
		for _, id := range lf.get(groupRemoved, mimeType) {
			if !committed[id] {
				removed[id] = true
			}
		}
	}

	// Legacy MimeType= cache from the scanned .desktop entries.
	for _, id := range r.legacyIDs(mimeType) {
		if !aseen[id] && !removed[id] {
			aseen[id] = true
			added = append(added, id)
		}
	}

	out := make([]string, 0, len(defaults)+len(added))
	seen := map[string]bool{}
	for _, id := range defaults {
		if !seen[id] && !removed[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range added {
		if !seen[id] && !removed[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// legacyIDs returns the desktop-file ids of installed entries whose
// MimeType= key advertises mimeType, sorted for determinism.
func (r *Resolver) legacyIDs(mimeType string) []string {
	var ids []string
	for id, e := range r.entries {
		for _, mt := range e.MimeType {
			if mt == mimeType {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// SetDefault makes id the default application for mimeType, replacing the
// [Default Applications] entry in the user's mimeapps.list, and persists the
// change. It returns [ErrNotFound] when id is not an installed, showable
// application.
func (r *Resolver) SetDefault(mimeType, id string) error {
	if r.lookup(id) == nil {
		return ErrNotFound
	}
	r.user.set(groupDefault, mimeType, []string{id})
	// A default is also an association; make sure it is not blacklisted.
	r.user.dropValue(groupRemoved, mimeType, id)
	return r.user.save()
}

// AddAssociation adds id to the [Added Associations] for mimeType in the
// user's mimeapps.list (and clears any matching [Removed Associations]
// entry), then persists. It returns [ErrNotFound] when id is not an
// installed, showable application.
func (r *Resolver) AddAssociation(mimeType, id string) error {
	if r.lookup(id) == nil {
		return ErrNotFound
	}
	r.user.addValue(groupAdded, mimeType, id)
	r.user.dropValue(groupRemoved, mimeType, id)
	return r.user.save()
}

// RemoveAssociation records id in the [Removed Associations] for mimeType in
// the user's mimeapps.list, drops it from that file's [Added Associations]
// and [Default Applications], then persists. Unlike the other writers it does
// not require id to be installed, so a stale association can be cleared.
func (r *Resolver) RemoveAssociation(mimeType, id string) error {
	r.user.addValue(groupRemoved, mimeType, id)
	r.user.dropValue(groupAdded, mimeType, id)
	r.user.dropValue(groupDefault, mimeType, id)
	return r.user.save()
}

// configDirs returns the mimeapps.list config search path, most important
// first: $XDG_CONFIG_HOME then each $XDG_CONFIG_DIRS.
func configDirs() []string {
	dirs := []string{xdg.ConfigHome}
	return append(dirs, xdg.ConfigDirs...)
}

// appDirs returns the applications/ search path, most important first:
// $XDG_DATA_HOME/applications then each $XDG_DATA_DIRS/applications.
func appDirs() []string {
	dirs := []string{filepath.Join(xdg.DataHome, "applications")}
	for _, d := range xdg.DataDirs {
		dirs = append(dirs, filepath.Join(d, "applications"))
	}
	return dirs
}

// currentDesktops splits $XDG_CURRENT_DESKTOP into its ordered components.
func currentDesktops() []string {
	v := os.Getenv("XDG_CURRENT_DESKTOP")
	if v == "" {
		return nil
	}
	var out []string
	for _, d := range strings.Split(v, ":") {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// showableIn reports whether e is visible in at least one of the current
// desktops (honoring OnlyShowIn/NotShowIn). An empty desktops list means no
// desktop filtering, so every entry is showable.
func showableIn(e *desktopentry.Entry, desktops []string) bool {
	if len(desktops) == 0 {
		return true
	}
	for _, d := range desktops {
		if e.ShouldShowIn(d) {
			return true
		}
	}
	return false
}

// reversed returns a copy of in with the order reversed.
func reversed(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}
