package app

import (
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/daemon"
	daemonsrv "github.com/iml885203/orbit/internal/daemon"
	"github.com/spf13/cobra"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check system health and diagnose common issues",
		RunE:  runDoctor,
	}
}

func runDoctor(_ *cobra.Command, _ []string) error {
	return runDoctorWithOptions(doctorOptions{showDaemon: true})
}

type doctorOptions struct {
	showDaemon bool
}

func runDoctorWithOptions(options doctorOptions) error {
	// If daemon is running, delegate to API for consistent results
	client := daemon.NewClient(daemon.DefaultSocketPath())
	if cli.JSONOutput {
		resp := doctorResponse(client)
		return cli.WriteJSONSuccess(os.Stdout, commandString(), resp, doctorRecommendedActions(resp))
	}
	if client.Health() == nil {
		resp, err := client.Doctor()
		if err == nil {
			_, _ = cli.Bold.Println("Checks:")
			for _, c := range resp.Checks {
				if !options.showDaemon && c.Name == "Daemon" {
					continue
				}
				icon := cli.Faint.Sprint("—")
				switch c.Status {
				case "pass":
					icon = cli.Green.Sprint("✓")
				case "fail":
					icon = cli.Red.Sprint("✗")
				case "warn":
					icon = cli.Yellow.Sprint("!")
				}
				fmt.Printf("  %s %s: %s\n", icon, c.Name, c.Message)
				if c.Hint != "" && (c.Status == "fail" || c.Status == "warn") {
					_, _ = cli.Faint.Printf("      → %s\n", c.Hint)
				}
			}
			return nil
		}
	}

	// Fallback: run checks locally
	pass := cli.Green.Sprint("✓")
	fail := cli.Red.Sprint("✗")

	_, _ = cli.Bold.Println("Checks:")

	cfg, err := config.Load(configFile)
	if err != nil {
		fmt.Printf("  %s Config file error: %v\n", fail, err)
	} else {
		fmt.Printf("  %s Config file found (%s)\n", pass, configFile)
		if err := config.Validate(cfg); err != nil {
			fmt.Printf("  %s Config validation failed: %v\n", fail, err)
			// Match localDoctorResponse: never evaluate the DB gate against
			// a config the tool just declared invalid.
			cfg = nil
		} else {
			fmt.Printf("  %s Config is valid\n", pass)
		}
	}

	if cfg != nil && len(cfg.Containers) > 0 {
		dc := daemonsrv.DockerCheck()
		dockerIcon := pass
		if dc.Status == daemon.CheckFail {
			dockerIcon = fail
		}
		fmt.Printf("  %s %s: %s\n", dockerIcon, dc.Name, dc.Message)
		if dc.Hint != "" && dc.Status == daemon.CheckFail {
			_, _ = cli.Faint.Printf("      → %s\n", dc.Hint)
		}
	}

	daemonClient := daemon.NewClient(daemon.DefaultSocketPath())
	daemonRunning := daemonClient.Health() == nil

	if cfg != nil && !daemonRunning {
		type portEntry struct {
			port int
			name string
		}
		var ports []portEntry
		for name, c := range cfg.Containers {
			for _, p := range c.Ports {
				ports = append(ports, portEntry{p.Host, name})
			}
		}
		for name, s := range cfg.Services {
			for _, p := range s.Ports {
				ports = append(ports, portEntry{p.Host, name})
			}
		}
		sort.Slice(ports, func(i, j int) bool { return ports[i].port < ports[j].port })
		for _, pe := range ports {
			ln, err := net.Listen("tcp", fmt.Sprintf(":%d", pe.port))
			if err != nil {
				fmt.Printf("  %s Port %d is already in use (needed by %s)\n", fail, pe.port, pe.name)
			} else {
				_ = ln.Close()
				fmt.Printf("  %s Port %d is available (%s)\n", pass, pe.port, pe.name)
			}
		}
	}

	if cfg != nil {
		for _, check := range daemonsrv.HostEnvironmentChecks(cfg) {
			icon := pass
			switch check.Status {
			case daemon.CheckFail:
				icon = fail
			case daemon.CheckWarn:
				icon = cli.Yellow.Sprint("!")
			}
			fmt.Printf("  %s %s: %s\n", icon, check.Name, check.Message)
			if check.Hint != "" && check.Status != daemon.CheckPass {
				_, _ = cli.Faint.Printf("      → %s\n", check.Hint)
			}
		}
	}

	if options.showDaemon && daemonRunning {
		status, err := daemonClient.Status()
		if err != nil {
			fmt.Printf("  %s Orbit daemon is running but status unavailable (%v)\n", fail, err)
		} else {
			healthy := 0
			for i := range status.Services {
				svc := &status.Services[i]
				if svc.State == "healthy" {
					healthy++
				}
			}
			fmt.Printf("  %s Orbit daemon is running (%d/%d services healthy)\n", pass, healthy, len(status.Services))
		}
	} else if options.showDaemon {
		fmt.Printf("  %s Orbit daemon is not running\n", cli.Faint.Sprint("—"))
	}

	// A nil cfg means config loading failed — that error is already printed
	// above, and claiming "no sql-server container" about an env we couldn't
	// read would mislead.
	if cfg == nil {
		return nil
	}
	for _, ext := range extensions {
		if ext.CLIDoctor != nil {
			ext.CLIDoctor.PrintHuman(cfg)
		}
	}
	return nil
}

func doctorResponse(client *daemon.Client) *daemon.DoctorResponse {
	if client.Health() == nil {
		if resp, err := client.Doctor(); err == nil {
			return resp
		}
	}
	return localDoctorResponse()
}

func localDoctorResponse() *daemon.DoctorResponse {
	var checks []daemon.DoctorCheck
	var cfg *config.Config
	if loaded, err := config.Load(configFile); err != nil {
		checks = append(checks, daemon.DoctorCheck{Name: "Config", Status: daemon.CheckFail, Message: err.Error()})
	} else if err := config.Validate(loaded); err != nil {
		checks = append(checks, daemon.DoctorCheck{Name: "Config", Status: daemon.CheckFail, Message: err.Error()})
	} else {
		cfg = loaded
		checks = append(checks, daemon.DoctorCheck{Name: "Config", Status: daemon.CheckPass, Message: configFile})
		if len(cfg.Containers) > 0 {
			checks = append(checks, daemonsrv.DockerCheck())
		}
		checks = append(checks, daemonsrv.HostEnvironmentChecks(cfg)...)
	}
	checks = append(checks, daemon.DoctorCheck{Name: "Daemon", Status: daemon.CheckInfo, Message: "not running"})
	// Feature-owned offline checks (the DB workflow) come from the
	// extensions' CLIDoctor hooks; a nil cfg is reported by the Config
	// fail check above, so features aren't asked to evaluate it.
	if cfg != nil {
		for _, ext := range extensions {
			if ext.CLIDoctor != nil {
				checks = append(checks, ext.CLIDoctor.Checks(cfg)...)
			}
		}
	}
	return &daemon.DoctorResponse{
		Checks: checks,
		RanAt:  time.Now().UTC().Format(time.RFC3339),
	}
}

func doctorRecommendedActions(resp *daemon.DoctorResponse) []cli.JSONAction {
	actions := []cli.JSONAction{cli.StatusAction()}
	if resp == nil {
		return append(actions, cli.DoctorAction())
	}
	added := map[string]bool{"orbit status --json": true}
	for _, check := range resp.Checks {
		if check.Status == daemon.CheckFail || check.Status == daemon.CheckWarn {
			if !added["orbit doctor --json"] {
				actions = append(actions, cli.DoctorAction())
				added["orbit doctor --json"] = true
			}
			if cmd, ok := strings.CutPrefix(check.Hint, "run: "); ok {
				cmd = strings.TrimSpace(cmd)
				if cmd != "" && !added[cmd] {
					actions = append(actions, cli.JSONAction{
						Command:     cmd,
						Reason:      "Apply doctor hint for " + check.Name + ".",
						Destructive: false,
					})
					added[cmd] = true
				}
			}
		}
	}
	return actions
}
