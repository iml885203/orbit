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
		return cli.NewOrbitNotRunningError()
	}

	url := fmt.Sprintf("http://localhost:%d", daemon.DashboardPort())
	return openURL(url, "dashboard", "")
}

type openJSONData struct {
	URL      string `json:"url"`
	Target   string `json:"target"`
	Resource string `json:"resource,omitempty"`
	Service  string `json:"service,omitempty"`
	Opened   bool   `json:"opened"`
}

var openBrowser = platform.OpenBrowser

func openURL(url, target, resource string) error {
	if !cli.JSONOutput {
		fmt.Printf("Opening %s\n", url)
	}
	if err := openBrowser(url); err != nil {
		return err
	}
	if cli.JSONOutput {
		service := ""
		if target == "service" {
			service = resource
		}
		return writeOpenJSON(os.Stdout, commandString(), openJSONData{
			URL:      url,
			Target:   target,
			Resource: resource,
			Service:  service,
			Opened:   true,
		})
	}
	return nil
}

func writeOpenJSON(w io.Writer, command string, data openJSONData) error {
	return cli.WriteJSONSuccess(w, command, data, nil)
}
