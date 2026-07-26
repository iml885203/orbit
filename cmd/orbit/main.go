// The official Orbit binary wires the generic database and tunnel features
// into the neutral orchestration core.
package main

import (
	"github.com/iml885203/orbit/app"
)

// Set via -ldflags -X main.version / -X main.buildTime (CI release job
// and Makefile); app.Main formats them for --version and /api/version.
var (
	version   string
	buildTime string
)

func main() {
	app.Main(version, buildTime, dashboardFS(), Extensions())
}
