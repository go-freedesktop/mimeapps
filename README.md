# mimeapps — go-freedesktop

[![ci](https://github.com/go-freedesktop/mimeapps/actions/workflows/ci.yml/badge.svg)](https://github.com/go-freedesktop/mimeapps/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-freedesktop/mimeapps.svg)](https://pkg.go.dev/github.com/go-freedesktop/mimeapps)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

The freedesktop **[Association between MIME types and applications](https://specifications.freedesktop.org/mime-apps-spec/latest/)**
layer — the piece a file manager or an **"Open With…"** menu needs: given a
MIME type, **which application opens it by default, and what is the ordered
list of candidates?** Pure Go, **CGO-free**, building on the Wave-1
`desktopentry` library rather than re-parsing `.desktop` files.

## Scope — what this adds, and what it reuses

This module does **not** reinvent application discovery. It stands on:

- **[`github.com/go-freedesktop/desktopentry`](https://github.com/go-freedesktop/desktopentry)**
  (BSD-3) — enumerating installed applications (`Scan`/`ScanDirs`), the
  showable filter (`NoDisplay`/`Hidden`, `OnlyShowIn`/`NotShowIn`) and the
  legacy `MimeType=` associations.
- **[`github.com/adrg/xdg`](https://github.com/adrg/xdg)** (MIT) — the XDG
  base-directory resolution.

On top of those it builds the **association layer**:

- the full **`mimeapps.list` search order** — `$XDG_CONFIG_HOME`, each
  `$XDG_CONFIG_DIRS`, then the `applications/` subdirectory of
  `$XDG_DATA_HOME` and each `$XDG_DATA_DIRS` — including the higher-priority
  **`<desktop>-mimeapps.list`** variants named after each entry of
  `$XDG_CURRENT_DESKTOP` (lowercased, e.g. `gnome-mimeapps.list`);
- the three groups **`[Default Applications]`**, **`[Added Associations]`**
  and **`[Removed Associations]`**, with correct **cross-directory
  precedence** (a higher-priority default/added is committed and immune to a
  lower-priority removal; a higher-priority removal blacklists an id against
  lower-priority additions);
- **`DefaultApp`** — the default resolved per the spec algorithm: the first
  existing, showable default across the files in priority order, falling back
  to the first candidate;
- **`Candidates`** — the ordered candidate list: defaults, then added
  associations, then the legacy `MimeType=` keys, minus removed associations,
  each application listed once and validated to exist and be showable;
- **`SetDefault`** / **`AddAssociation`** / **`RemoveAssociation`** —
  round-trippable edits to the user's `mimeapps.list` that preserve its
  groups, key order and comments.

## Install

```sh
go get github.com/go-freedesktop/mimeapps
```

## Quickstart

```go
package main

import (
	"fmt"

	"github.com/go-freedesktop/mimeapps"
)

func main() {
	r := mimeapps.Load() // standard XDG environment + $XDG_CURRENT_DESKTOP

	// The default handler for a MIME type.
	if app, err := r.DefaultApp("text/html"); err == nil {
		fmt.Println("default:", app.Name, "→", app.Exec)
	}

	// The ordered "Open With…" menu.
	for _, app := range r.Candidates("image/png") {
		fmt.Println("candidate:", app.ID, app.Name)
	}

	// Make an application the user's default (persisted to mimeapps.list).
	_ = r.SetDefault("text/html", "org.mozilla.firefox.desktop")
}
```

The returned values are `*desktopentry.Entry`, so a launcher can go straight
to `ExpandExec` to build the argv.

## Public API

| Symbol | Purpose |
| --- | --- |
| `Load() *Resolver` | build from the standard XDG env + `$XDG_CURRENT_DESKTOP` |
| `LoadDirs(configDirs, appDirs, desktops) *Resolver` | injectable form (for tests / sandboxes) |
| `(*Resolver).DefaultApp(mime) (*desktopentry.Entry, error)` | default handler, per the spec algorithm |
| `(*Resolver).Candidates(mime) []*desktopentry.Entry` | ordered candidate list |
| `(*Resolver).SetDefault(mime, id) error` | set the default (round-trippable write) |
| `(*Resolver).AddAssociation(mime, id) error` | add an association |
| `(*Resolver).RemoveAssociation(mime, id) error` | remove / blacklist an association |
| `ErrNoDefault`, `ErrNotFound` | resolution / write error sentinels |

`id` is a full desktop-file id such as `firefox.desktop`.

## wasmdesk / wasmbox integration

This library is the resolver behind an **"Open With…"** action in the wasmdesk
compositor:

- **`DefaultApp(mime)`** picks the app to launch when a file is
  double-clicked; the returned `*desktopentry.Entry` feeds
  `desktopentry.ExpandExec` to produce the argv.
- **`Candidates(mime)`** populates the right-click **"Open With ▸"** submenu.
- **`SetDefault(mime, id)`** persists the user's choice from that menu's
  *"Set as default"* checkbox back to `mimeapps.list`.

## Tests & coverage

`CGO_ENABLED=0 go test ./...` — **100% statement coverage**, including every
error branch, driven by fixtures under `testdata/` (layered config/data dirs,
a `<desktop>-mimeapps.list`, `NoDisplay`/`OnlyShowIn` apps, a removed
association and a dangling default). CI additionally cross-builds and runs the
suite on the six supported 64-bit targets (amd64/arm64 natively,
riscv64/loong64/ppc64le/s390x under qemu-user).

## License

BSD-3-Clause. Copyright (c) the go-freedesktop/mimeapps authors.

---

> **Note:** the `go-freedesktop` org landing page and MkDocs site are deferred
> to the Wave-2 documentation sweep; this repo ships the README and `.github`
> profile for now.
