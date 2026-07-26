package main

import (
	"embed"
	"io/fs"
)

// The built dashboard (make ui → dist/, gitignored) embeds into the binary
// here because the main package owns the distributable artifact. The core
// daemon serves whatever fs.FS it is handed.
//
//go:embed all:dist
var dashboardFiles embed.FS

// dashboardFS roots the embed at the dist contents for app.Main.
func dashboardFS() fs.FS {
	sub, err := fs.Sub(dashboardFiles, "dist")
	if err != nil {
		panic(err) // embed guarantees dist exists at compile time
	}
	return sub
}
