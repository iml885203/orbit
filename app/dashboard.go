package app

import (
	"fmt"

	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/platform"
)

func runDashboard() error {
	client := daemon.NewClient(daemon.DefaultSocketPath())
	if err := client.Health(); err != nil {
		return fmt.Errorf("no daemon running — start with 'orbit up' or 'orbit daemon start'")
	}

	url := fmt.Sprintf("http://localhost:%d", daemon.DashboardPort())
	fmt.Printf("Opening %s\n", url)
	return platform.OpenBrowser(url)
}
