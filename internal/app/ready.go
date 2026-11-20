package app

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/iml885203/orbit/internal/cli"
	"github.com/iml885203/orbit/internal/config"
	"github.com/iml885203/orbit/internal/daemon"
	"github.com/iml885203/orbit/internal/engine"
	"github.com/iml885203/orbit/internal/instance"
	"github.com/iml885203/orbit/internal/shellquote"
	"github.com/spf13/cobra"
)

type readyJSONData struct {
	Ready             bool              `json:"ready"`
	Summary           string            `json:"summary"`
	RequiredResources []string          `json:"required_resources"`
	Resources         []readyResource   `json:"resources"`
	Observation       *readyObservation `json:"observation,omitempty"`
}

type readyResource struct {
	Name        string         `json:"name"`
	State       string         `json:"state"`
	Reason      string         `json:"reason"`
	Ports       map[string]int `json:"ports,omitempty"`
	URL         string         `json:"url,omitempty"`
	URLRedacted bool           `json:"url_redacted,omitempty"`
}

type readyObservation struct {
	ConfigPath     string    `json:"config_path"`
	Instance       string    `json:"instance,omitempty"`
	Epoch          int64     `json:"epoch"`
	Generation     uint64    `json:"generation"`
	ObservedAt     time.Time `json:"observed_at"`
	EndpointSource string    `json:"endpoint_source"`
}

func readyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ready <resource>...",
		Short: "Assert that explicitly required resources are healthy",
		Long: "Check only named configured containers or host services in the selected environment and instance.\n" +
			"Exit nonzero if any required resource is not healthy or runtime evidence is unavailable.\n" +
			"This read-only snapshot never starts, switches, waits for or prepares resources.\n" +
			"It verifies infrastructure readiness; your harness must still test the application.",
		Args: cobra.MinimumNArgs(1),
		RunE: runReady,
	}
}

func runReady(cmd *cobra.Command, names []string) error {
	data := readyJSONData{RequiredResources: []string{}, Resources: []readyResource{}}
	seen := map[string]bool{}
	for _, name := range names {
		if !seen[name] {
			seen[name] = true
			data.RequiredResources = append(data.RequiredResources, name)
			data.Resources = append(data.Resources, readyResource{Name: name, State: "unavailable", Reason: "Runtime observation is unavailable."})
		}
	}
	err := observeRequiredResources(cmd, &data)
	if err != nil {
		data.Summary = err.Error()
	} else {
		data.Ready = true
		data.Summary = "All required resources are healthy."
	}
	actions := readyRecoveryActions(err)
	failure := err
	if err != nil && actions != nil {
		failure = readyFailure{cause: err, actions: actions}
	}
	if cli.JSONOutput {
		if err != nil {
			if writeErr := cli.WriteJSONFailure(cmd.OutOrStdout(), commandString(), data, failure, nil); writeErr != nil {
				return writeErr
			}
			return errCLIJSONAlreadyRendered{err: failure}
		}
		return cli.WriteJSONSuccess(cmd.OutOrStdout(), commandString(), data, nil)
	}
	summary, hint, next := data.Summary, "", ""
	if failure != nil {
		headline, guidance, command := cli.HumanGuidance(failure)
		if headline != "" {
			summary = headline
		}
		hint, next = guidance, command
		if next == "" {
			next = cli.FirstRecommendedCommand(failure)
		}
	}
	if _, writeErr := fmt.Fprintln(cmd.OutOrStdout(), summary); writeErr != nil {
		return fmt.Errorf("writing readiness: %w", writeErr)
	}
	for _, resource := range data.Resources {
		if _, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s — %s\n", resource.Name, resource.State, resource.Reason); writeErr != nil {
			return fmt.Errorf("writing readiness: %w", writeErr)
		}
	}
	if hint != "" {
		if _, writeErr := fmt.Fprintln(cmd.OutOrStdout(), "  "+hint); writeErr != nil {
			return fmt.Errorf("writing readiness guidance: %w", writeErr)
		}
	}
	if next != "" {
		if _, writeErr := fmt.Fprintln(cmd.OutOrStdout(), "  Next: "+next); writeErr != nil {
			return fmt.Errorf("writing readiness recovery: %w", writeErr)
		}
	}
	if err != nil {
		return errCLIHumanAlreadyRendered{err: err}
	}
	return nil
}

func observeRequiredResources(cmd *cobra.Command, data *readyJSONData) error {
	selection, err := readOnlyEnvironmentSelection()
	if err != nil {
		return err
	}
	if environmentSelectionBlocksConfig(selection, configFile) {
		return newEnvironmentSelectionRequiredError(selection)
	}
	cfg, err := config.Load(configFile)
	if err != nil {
		return err
	}
	for _, name := range data.RequiredResources {
		_, container := cfg.Containers[name]
		_, service := cfg.Services[name]
		if !container && !service {
			return cli.NewUnknownResourceError(name)
		}
	}
	client := daemon.NewClient(daemon.DefaultSocketPath()).WithContext(cmd.Context())
	status, err := client.Status()
	if err != nil {
		return fmt.Errorf("reading readiness snapshot: %w", err)
	}
	if err := daemon.CheckConfigMatch(configFile, status.ConfigPath); err != nil {
		return err
	}
	if status.Epoch <= 0 {
		return readyEvidenceError("daemon returned no runtime epoch")
	}
	if status.Instance != instance.CurrentName() {
		return readyEvidenceError("daemon returned a different instance")
	}
	if err := daemon.CheckEnvironmentReconciled(status); err != nil {
		return err
	}
	version, err := client.Version()
	if err != nil {
		return fmt.Errorf("reading daemon version: %w", err)
	}
	if version.Running == "" || version.Running != buildVersion() {
		return readyEvidenceError(fmt.Sprintf("CLI build %q and daemon build %q differ; use matching Orbit builds before checking readiness", buildVersion(), version.Running))
	}
	observed := make(map[string]*daemon.ResourceStatus, len(status.Resources))
	for i := range status.Resources {
		resource := &status.Resources[i]
		if _, exists := observed[resource.Name]; exists {
			return readyEvidenceError("daemon returned duplicate resource identities")
		}
		observed[resource.Name] = resource
	}
	data.Observation = &readyObservation{ConfigPath: status.ConfigPath, Instance: status.Instance, Epoch: status.Epoch, Generation: status.Generation, ObservedAt: time.Now().UTC(), EndpointSource: "daemon_status"}
	healthy := true
	for i, name := range data.RequiredResources {
		resource, exists := observed[name]
		if !exists {
			healthy = false
			data.Resources[i].Reason = "Required resource is absent from the runtime snapshot."
			continue
		}
		if resource.State != "healthy" {
			healthy = false
		}
		reason := readinessResourceReason(resource)
		endpointURL := readinessEndpointURL(resource.URL)
		data.Resources[i] = readyResource{Name: name, State: resource.State, Reason: reason, Ports: resource.Ports, URL: endpointURL, URLRedacted: endpointURL != resource.URL}
	}
	if !healthy {
		return cli.NewChecksFailedError("Required resources are not healthy.")
	}
	return nil
}

func readinessResourceReason(resource *daemon.ResourceStatus) string {
	switch resource.State {
	case "healthy":
		return "Required resource is healthy."
	case "stopped":
		return "Required resource is stopped."
	case "pending":
		return "Required resource is waiting for dependencies."
	case "degraded":
		switch resource.FailureKind {
		case string(engine.FailureKindHealth):
			return "Required resource failed its health check."
		case string(engine.FailureKindProcess):
			return "Required process exited."
		case string(engine.FailureKindBuild):
			return "Required resource build failed."
		case string(engine.FailureKindRuntime):
			return "Required runtime is unavailable."
		}
	}
	return "Required resource is not healthy."
}

func readinessEndpointURL(raw string) string {
	if raw == "" {
		return ""
	}
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return ""
	}
	endpoint.User = nil
	endpoint.RawQuery, endpoint.Fragment, endpoint.RawFragment = "", "", ""
	endpoint.ForceQuery = false
	return endpoint.String()
}

type readyEvidenceError string

func (e readyEvidenceError) Error() string     { return string(e) }
func (e readyEvidenceError) ErrorCode() string { return "readiness_unavailable" }

type readyFailure struct {
	cause   error
	actions []cli.JSONAction
}

func (e readyFailure) Error() string { return e.cause.Error() }
func (e readyFailure) Unwrap() error { return e.cause }
func (e readyFailure) CLIJSONHint() string {
	return "Inspect the reported conditions before explicitly changing the selected runtime; then repeat the readiness assertion."
}
func (e readyFailure) CLIJSONReplacementActions() []cli.JSONAction { return e.actions }

func readyRecoveryActions(err error) []cli.JSONAction {
	if err == nil {
		return nil
	}
	var args []string
	var reason string
	var stale *daemon.ConfigStaleError
	var mismatch *daemon.ConfigMismatchError
	var evidence readyEvidenceError
	switch {
	case errors.As(err, &stale):
		args, reason = []string{"env", "apply"}, "Apply pending environment changes explicitly, then repeat the readiness check."
	case errors.As(err, &mismatch):
		args, reason = []string{"up"}, "Start or switch to the requested environment explicitly."
	case errors.As(err, &evidence):
		args, reason = []string{"status"}, "Inspect the selected runtime identity and align Orbit builds before repeating the check."
	case errors.Is(err, daemon.ErrDaemonUnreachable):
		args, reason = []string{"up"}, "Start the requested environment explicitly, then repeat the readiness check."
	case errors.Is(err, cli.ErrChecksFailed):
		args, reason = []string{"up"}, "Restore the requested environment explicitly, then repeat the readiness check."
	case errors.Is(err, cli.ErrUnknownResource):
		args, reason = []string{"status"}, "Inspect configured names in the selected environment."
	default:
		return nil
	}
	parts := []string{"orbit"}
	if name := instance.CurrentName(); name != "" {
		parts = append(parts, "--instance", name)
	}
	if configFile != "" {
		parts = append(parts, "--config", configFile)
	}
	parts = append(parts, args...)
	parts = append(parts, "--json")
	for i := 1; i < len(parts); i++ {
		parts[i] = shellquote.Quote(parts[i])
	}
	return []cli.JSONAction{{Command: strings.Join(parts, " "), Reason: reason}}
}
