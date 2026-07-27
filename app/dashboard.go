package app

import (
	"fmt"
	"io"
	"os"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/daemon"
	"github.com/iml885203/orbit/platform"
)

func runDashboard() error {
	client := daemon.NewClient(daemon.DefaultSocketPath())
	if err := client.Health(); err != nil {
		return fmt.Errorf("no daemon running — start with 'orbit up' or 'orbit daemon start'")
	}

	url := fmt.Sprintf("http://localhost:%d", daemon.DashboardPort())
	return openURL(url, "dashboard", "")
}

type openJSONData struct {
	URL     string `json:"url"`
	Target  string `json:"target"`
	Service string `json:"service,omitempty"`
	Opened  bool   `json:"opened"`
}

func openURL(url, target, service string) error {
	if !cli.JSONOutput {
		fmt.Printf("Opening %s\n", url)
	}
	if err := platform.OpenBrowser(url); err != nil {
		return err
	}
	if cli.JSONOutput {
		return writeOpenJSON(os.Stdout, commandString(), openJSONData{
			URL:     url,
			Target:  target,
			Service: service,
			Opened:  true,
		})
	}
	return nil
}

func writeOpenJSON(w io.Writer, command string, data openJSONData) error {
	return cli.WriteJSONSuccess(w, command, data, nil)
}
