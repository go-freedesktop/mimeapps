// Copyright (c) the go-freedesktop/mimeapps authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mimeapps

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-freedesktop/desktopentry"
)

// fixture directory sets used across the resolution tests.
var (
	fxConfig = []string{"testdata/config-home", "testdata/config-sys"}
	fxApps   = []string{"testdata/apps-home", "testdata/apps-sys"}
)

// names maps a candidate list to the underlying application names.
func names(entries []*desktopentry.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestDefaultAppDesktopPrefix proves the <desktop>-mimeapps.list variant
// outranks the plain config-home file: under GNOME the gnome-mimeapps.list
// default (Browser) wins over the plain default (Editor).
func TestDefaultAppDesktopPrefix(t *testing.T) {
	r := LoadDirs(fxConfig, fxApps, []string{"GNOME"})

	e, err := r.DefaultApp("text/plain")
	if err != nil {
		t.Fatalf("DefaultApp: %v", err)
	}
	if e.Name != "Example Browser" {
		t.Errorf("gnome default = %q, want Example Browser", e.Name)
	}
}

// TestDefaultAppPlainAndPrecedence checks that without a matching desktop
// prefix the plain config-home default wins over the system default.
func TestDefaultAppPlainAndPrecedence(t *testing.T) {
	r := LoadDirs(fxConfig, fxApps, nil)

	e, err := r.DefaultApp("text/plain")
	if err != nil {
		t.Fatalf("DefaultApp: %v", err)
	}
	if e.Name != "Example Editor" {
		t.Errorf("default = %q, want Example Editor (config-home over config-sys)", e.Name)
	}

	// text/html has no config-home default, only Added; the system default
	// (Browser) supplies it.
	e, err = r.DefaultApp("text/html")
	if err != nil || e.Name != "Example Browser" {
		t.Errorf("text/html default = %v, %v; want Example Browser", nameOf(e), err)
	}

	// image/png default is the config-home Viewer.
	e, _ = r.DefaultApp("image/png")
	if e.Name != "Example Viewer" {
		t.Errorf("image/png default = %q, want Example Viewer", e.Name)
	}
}

// TestDefaultAppFallbackToCandidate covers the branch where no [Default
// Applications] entry resolves and the first candidate is returned:
// text/markdown is advertised only via a MimeType= key.
func TestDefaultAppFallbackToCandidate(t *testing.T) {
	r := LoadDirs(fxConfig, fxApps, nil)
	e, err := r.DefaultApp("text/markdown")
	if err != nil {
		t.Fatalf("DefaultApp(text/markdown): %v", err)
	}
	if e.Name != "Example Editor" {
		t.Errorf("fallback default = %q, want Example Editor", e.Name)
	}
}

// TestDefaultAppNoDefault covers ErrNoDefault: application/pdf points only at
// a non-installed desktop id (Ghost), so nothing resolves.
func TestDefaultAppNoDefault(t *testing.T) {
	r := LoadDirs(fxConfig, fxApps, nil)
	_, err := r.DefaultApp("application/pdf")
	if !errors.Is(err, ErrNoDefault) {
		t.Fatalf("err = %v, want ErrNoDefault", err)
	}
}

// TestCandidatesOrdering asserts the exact candidate ordering: defaults
// first, then added, then the legacy MimeType= entries, with removed
// associations dropped and non-installed / hidden ids filtered out.
func TestCandidatesOrdering(t *testing.T) {
	// Under GNOME the gnome default (Browser) leads, Editor follows, and
	// GnomeOnly (OnlyShowIn=GNOME, via MimeType=) is showable so it trails.
	// SysText is removed by the config-home [Removed Associations].
	got := names(LoadDirs(fxConfig, fxApps, []string{"GNOME"}).Candidates("text/plain"))
	want := []string{"Example Browser", "Example Editor", "Gnome Only"}
	if !eq(got, want) {
		t.Errorf("GNOME candidates = %v, want %v", got, want)
	}

	// Without a desktop filter GnomeOnly is still showable, but the leading
	// default is now Editor (no gnome file loaded).
	got = names(LoadDirs(fxConfig, fxApps, nil).Candidates("text/plain"))
	want = []string{"Example Editor", "Gnome Only"}
	if !eq(got, want) {
		t.Errorf("no-desktop candidates = %v, want %v", got, want)
	}

	// Under KDE, GnomeOnly is not showable and disappears entirely.
	got = names(LoadDirs(fxConfig, fxApps, []string{"KDE"}).Candidates("text/plain"))
	want = []string{"Example Editor"}
	if !eq(got, want) {
		t.Errorf("KDE candidates = %v, want %v", got, want)
	}

	// image/png: default Viewer first, then Browser via its MimeType= key.
	got = names(LoadDirs(fxConfig, fxApps, nil).Candidates("image/png"))
	want = []string{"Example Viewer", "Example Browser"}
	if !eq(got, want) {
		t.Errorf("image/png candidates = %v, want %v", got, want)
	}

	// text/html resolves to a single de-duplicated Browser.
	got = names(LoadDirs(fxConfig, fxApps, nil).Candidates("text/html"))
	want = []string{"Example Browser"}
	if !eq(got, want) {
		t.Errorf("text/html candidates = %v, want %v", got, want)
	}

	// application/pdf: only a non-installed id, so no candidates.
	if got := LoadDirs(fxConfig, fxApps, nil).Candidates("application/pdf"); len(got) != 0 {
		t.Errorf("application/pdf candidates = %v, want none", names(got))
	}
}

// TestWritersRoundTrip exercises SetDefault / AddAssociation /
// RemoveAssociation against a fresh (initially absent) user mimeapps.list and
// checks the edits both persist and take effect on reload.
func TestWritersRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	cfg := []string{tmp, "testdata/config-sys"}

	r := LoadDirs(cfg, fxApps, nil)

	// SetDefault on an absent file creates it.
	if err := r.SetDefault("text/plain", "org.example.Editor.desktop"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	// Unknown id is rejected.
	if err := r.SetDefault("text/plain", "org.example.Ghost.desktop"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetDefault ghost err = %v, want ErrNotFound", err)
	}

	// AddAssociation, idempotent second call, and unknown-id rejection.
	if err := r.AddAssociation("image/png", "org.example.Browser.desktop"); err != nil {
		t.Fatalf("AddAssociation: %v", err)
	}
	if err := r.AddAssociation("image/png", "org.example.Browser.desktop"); err != nil {
		t.Fatalf("AddAssociation (repeat): %v", err)
	}
	if err := r.AddAssociation("image/png", "org.example.Ghost.desktop"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AddAssociation ghost err = %v, want ErrNotFound", err)
	}

	// RemoveAssociation of an id that is present in Added (drops it) and one
	// that is not (no-op drop path).
	if err := r.RemoveAssociation("image/png", "org.example.Browser.desktop"); err != nil {
		t.Fatalf("RemoveAssociation: %v", err)
	}
	if err := r.RemoveAssociation("text/plain", "org.example.SysText.desktop"); err != nil {
		t.Fatalf("RemoveAssociation absent: %v", err)
	}

	// The file now exists and round-trips through a fresh resolver.
	if _, err := os.Stat(filepath.Join(tmp, "mimeapps.list")); err != nil {
		t.Fatalf("user mimeapps.list not written: %v", err)
	}
	r2 := LoadDirs(cfg, fxApps, nil)
	e, err := r2.DefaultApp("text/plain")
	if err != nil || e.Name != "Example Editor" {
		t.Errorf("reloaded default = %v, %v; want Example Editor", nameOf(e), err)
	}
	// Browser was added then removed for image/png; it must not be a
	// candidate, but the Viewer default remains.
	got := names(r2.Candidates("image/png"))
	for _, n := range got {
		if n == "Example Browser" {
			t.Errorf("Browser should have been removed: %v", got)
		}
	}
}

// TestWriterSaveError covers the save error path (MkdirAll fails because a
// path component is a regular file).
func TestWriterSaveError(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := LoadDirs([]string{tmp}, fxApps, nil)
	// Redirect the user file under a path whose parent is a regular file.
	r.user.path = filepath.Join(blocker, "sub", "mimeapps.list")
	if err := r.SetDefault("text/plain", "org.example.Editor.desktop"); err == nil {
		t.Fatal("expected save error, got nil")
	}
}

// TestShowableUnfilteredAndLookup covers the no-desktop showability branch
// and the entry index.
func TestShowableUnfilteredAndLookup(t *testing.T) {
	r := LoadDirs(fxConfig, fxApps, nil)
	if r.lookup("org.example.Ghost.desktop") != nil {
		t.Error("Ghost must not resolve")
	}
	if r.lookup("org.example.Editor.desktop") == nil {
		t.Error("Editor must resolve")
	}
	// Hidden (NoDisplay) is filtered out by the desktopentry scan.
	if r.lookup("org.example.Hidden.desktop") != nil {
		t.Error("NoDisplay entry must not resolve")
	}
}

// TestLoad exercises the environment-driven constructor and the XDG helper
// paths; content is host-dependent so we only assert it does not panic.
func TestLoad(t *testing.T) {
	if r := Load(); r == nil {
		t.Fatal("Load returned nil")
	}
	_ = configDirs()
	_ = appDirs()
}

// TestCurrentDesktops covers both the empty and populated env branches.
func TestCurrentDesktops(t *testing.T) {
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	if got := currentDesktops(); got != nil {
		t.Errorf("empty env = %v, want nil", got)
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME: KDE :")
	got := currentDesktops()
	if !eq(got, []string{"GNOME", "KDE"}) {
		t.Errorf("split = %v, want [GNOME KDE]", got)
	}
}

func nameOf(e *desktopentry.Entry) string {
	if e == nil {
		return "<nil>"
	}
	return e.Name
}
