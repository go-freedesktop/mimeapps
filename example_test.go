// Copyright (c) the go-freedesktop/mimeapps authors
//
// SPDX-License-Identifier: BSD-3-Clause

package mimeapps_test

import (
	"fmt"

	"github.com/go-freedesktop/mimeapps"
)

// ExampleResolver_Candidates shows resolving the ordered "Open With" list and
// the default application for a MIME type from an injectable set of
// directories (the same entry point tests use).
func ExampleResolver_Candidates() {
	r := mimeapps.LoadDirs(
		[]string{"testdata/config-home", "testdata/config-sys"},
		[]string{"testdata/apps-home", "testdata/apps-sys"},
		nil, // no XDG_CURRENT_DESKTOP filtering
	)

	def, _ := r.DefaultApp("image/png")
	fmt.Println("default:", def.Name)

	fmt.Print("candidates:")
	for _, e := range r.Candidates("image/png") {
		fmt.Print(" ", e.Name)
	}
	fmt.Println()
	// Output:
	// default: Example Viewer
	// candidates: Example Viewer Example Browser
}
