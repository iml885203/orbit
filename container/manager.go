package container

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/iml885203/orbit/config"
	"github.com/iml885203/orbit/dockerctx"
	"github.com/iml885203/orbit/logging"
	orbitport "github.com/iml885203/orbit/port"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	labelManaged = "orbit.managed"
	labelService = "orbit.service"
	labelConfig  = "orbit.config"
	networkName  = "orbit"
)

// Manager handles Docker container lifecycle.
type Manager struct {
	cli       *client.Client
	namespace string

	// OnOutput is called for container log lines.
	OnOutput func(name string, line string)
	// OnAction is called to narrate lifecycle actions (start/stop/pull/...).
	// Passed through the logging multiplexer so the dashboard can show it.
	OnAction func(name string, msg string)
}

func (m *Manager) narrate(name, msg string) {
	if m.OnAction != nil {
		m.OnAction(name, msg)
	}
}

// Namespace returns the orbit namespace this Manager operates under.
// Empty means the default (un-namespaced) instance.
func (m *Manager) Namespace() string { return m.namespace }

// ContainerName returns the Docker container name for a service, scoped
// to this Manager's namespace.
func (m *Manager) ContainerName(svc string) string {
	return ContainerName(m.namespace, svc)
}

func NewManager(namespace string) (*Manager, error) {
	cli, err := dockerctx.NewClient()
	if err != nil {
		return nil, fmt.Errorf("connecting to Docker: %w", err)
	}
	return &Manager{cli: cli, namespace: namespace}, nil
}

// EnsureNetwork creates the orbit Docker network if it doesn't exist.
func (m *Manager) EnsureNetwork(ctx context.Context) error {
	networks, err := m.cli.NetworkList(ctx, client.NetworkListOptions{
		Filters: make(client.Filters).Add("name", networkName),
	})
	if err != nil {
		return err
	}
	if len(networks.Items) > 0 {
		return nil
	}
	_, err = m.cli.NetworkCreate(ctx, networkName, client.NetworkCreateOptions{
		Driver: "bridge",
	})
	return err
}

// Start creates and starts a container based on config.
func (m *Manager) Start(ctx context.Context, name string, cfg *config.Container) error {
	return m.start(ctx, name, cfg, "")
}

// start is the internal implementation. parentName, when non-empty, marks the
// container as a sidecar of the named parent so it can be cleaned up alongside
// the parent.
func (m *Manager) start(ctx context.Context, name string, cfg *config.Container, parentName string) error {
	m.narrate(name, fmt.Sprintf("start requested (image=%s)", cfg.Image))
	adopted, err := m.reconcileExisting(ctx, name, cfg, parentName)
	if err != nil {
		m.narrate(name, "ERROR: "+err.Error())
		return err
	}
	if adopted {
		return nil
	}
	if err := m.ensureImageAvailable(ctx, name, cfg); err != nil {
		m.narrate(name, "ERROR: "+err.Error())
		return err
	}
	if conflict := containerPortConflict(name, cfg); conflict != nil {
		m.narrate(name, "ERROR: "+conflict.Error())
		return conflict
	}

	// Build port bindings
	exposedPorts := network.PortSet{}
	portBindings := network.PortMap{}
	for _, pd := range cfg.Ports {
		containerPort := network.MustParsePort(strconv.Itoa(pd.Target) + "/tcp")
		exposedPorts[containerPort] = struct{}{}
		portBindings[containerPort] = []network.PortBinding{{
			HostIP:   netip.IPv4Unspecified(),
			HostPort: strconv.Itoa(pd.Host),
		}}
	}

	var envList []string
	for k, v := range cfg.Environment {
		envList = append(envList, k+"="+v)
	}

	labels := map[string]string{
		labelManaged: "true",
		labelService: name,
		labelConfig:  containerConfigFingerprint(cfg, parentName),
	}
	if m.namespace != "" {
		labels[labelNamespace] = m.namespace
	}
	if parentName != "" {
		labels[labelParent] = parentName
	}

	containerName := m.ContainerName(name)

	// Container config
	containerConfig := &container.Config{
		Image:        cfg.Image,
		ExposedPorts: exposedPorts,
		Env:          envList,
		Labels:       labels,
		Cmd:          cfg.Command,
		User:         cfg.User,
	}
	if len(cfg.Entrypoint) > 0 {
		containerConfig.Entrypoint = cfg.Entrypoint
	}

	// Platform (e.g. linux/amd64 for SQL Server on ARM Mac)
	var platform *ocispec.Platform
	if cfg.Platform != "" {
		platform = &ocispec.Platform{OS: "linux", Architecture: "amd64"}
		parts := splitPlatform(cfg.Platform)
		platform.OS = parts[0]
		platform.Architecture = parts[1]
	}

	// SQL Server on WSL2 requires seccomp=unconfined: SQLPAL's Windows
	// security emulation (lsass/samsrv) triggers syscalls that the default
	// WSL2 seccomp profile blocks, causing a fatal crash on startup.
	// Native Linux Docker uses a permissive-enough default profile, so this
	// is only needed on Windows where Docker runs inside WSL2.
	var securityOpt []string
	if runtime.GOOS == "windows" && cfg.Environment["ACCEPT_EULA"] != "" {
		securityOpt = []string{"seccomp=unconfined"}
	}

	portSummary := formatPortBindings(cfg.Ports)
	m.narrate(name, fmt.Sprintf("creating container %s (ports=%s)", containerName, portSummary))
	resp, err := m.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: containerConfig,
		HostConfig: &container.HostConfig{
			PortBindings: portBindings,
			Binds:        cfg.Volumes,
			NetworkMode:  container.NetworkMode(networkName),
			SecurityOpt:  securityOpt,
		},
		NetworkingConfig: &network.NetworkingConfig{},
		Platform:         platform,
		Name:             containerName,
	})
	if err != nil {
		m.narrate(name, "ERROR: "+err.Error())
		return fmt.Errorf("creating container %s: %w", name, err)
	}

	// Start container
	if _, err := m.cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		if conflict := containerPortConflict(name, cfg); conflict != nil {
			m.narrate(name, "ERROR: "+conflict.Error())
			return conflict
		}
		m.narrate(name, "ERROR: "+err.Error())
		return fmt.Errorf("starting container %s: %w", name, err)
	}
	m.narrate(name, "started")

	// Stream logs in background
	go m.streamLogs(ctx, name, resp.ID)

	return nil
}

func (m *Manager) reconcileExisting(
	ctx context.Context,
	name string,
	cfg *config.Container,
	parentName string,
) (bool, error) {
	containerName := m.ContainerName(name)
	inspect, err := m.cli.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspecting existing container %s: %w", containerName, err)
	}
	labels := inspect.Container.Config.Labels
	if labels[labelManaged] != "true" ||
		labels[labelService] != name ||
		!m.matchesNamespace(labels) {
		return false, fmt.Errorf(
			"container %s already exists but is not owned by this Orbit environment",
			containerName,
		)
	}

	fingerprint := containerConfigFingerprint(cfg, parentName)
	if inspect.Container.State.Running && labels[labelConfig] == fingerprint {
		m.narrate(name, "adopted existing managed container "+containerName)
		go m.streamLogs(ctx, name, inspect.Container.ID)
		return true, nil
	}

	// The container is ours, but it is stopped, from an older Orbit version,
	// or no longer matches the selected environment. Remove that known
	// ownership before checking ports so our own stale resource can never be
	// reported as an unrelated external conflict.
	m.narrate(name, "replacing stale managed container "+containerName)
	if _, err := m.cli.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{Force: true}); err != nil &&
		!cerrdefs.IsNotFound(err) {
		return false, fmt.Errorf("removing stale managed container %s: %w", containerName, err)
	}
	return false, nil
}

func containerConfigFingerprint(cfg *config.Container, parentName string) string {
	spec := struct {
		Image       string
		Command     []string
		Entrypoint  []string
		User        string
		Platform    string
		Ports       map[string]config.PortDef
		Environment map[string]string
		Volumes     []string
		Parent      string
	}{
		Image:       cfg.Image,
		Command:     cfg.Command,
		Entrypoint:  cfg.Entrypoint,
		User:        cfg.User,
		Platform:    cfg.Platform,
		Ports:       cfg.Ports,
		Environment: cfg.Environment,
		Volumes:     cfg.Volumes,
		Parent:      parentName,
	}
	encoded, _ := json.Marshal(spec)
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func containerPortConflict(name string, cfg *config.Container) *orbitport.ConflictError {
	if cfg == nil {
		return nil
	}
	ports := make([]int, 0, len(cfg.Ports))
	for _, definition := range cfg.Ports {
		ports = append(ports, definition.Host)
	}
	sort.Ints(ports)
	conflicts := orbitport.CheckPorts(map[string][]int{name: ports})
	if len(conflicts) == 0 {
		return nil
	}
	return orbitport.NewConflictError(conflicts[0])
}

func formatPortBindings(ports map[string]config.PortDef) string {
	if len(ports) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		parts = append(parts, strconv.Itoa(p.Host)+":"+strconv.Itoa(p.Target))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func (m *Manager) ensureImageAvailable(ctx context.Context, name string, cfg *config.Container) error {
	switch cfg.PullPolicy {
	case "never":
		exists, err := m.imageExists(ctx, cfg.Image)
		if err != nil {
			return fmt.Errorf("checking local image %s: %w", cfg.Image, err)
		}
		if !exists {
			return fmt.Errorf("image %s not found locally and pull_policy is set to never", cfg.Image)
		}
		return nil
	case "if_not_present":
		exists, err := m.imageExists(ctx, cfg.Image)
		if err != nil {
			return fmt.Errorf("checking local image %s: %w", cfg.Image, err)
		}
		if exists {
			return nil
		}
	}

	m.narrate(name, "pulling image "+cfg.Image)
	pullOpts := client.ImagePullOptions{}
	if cfg.Platform != "" {
		parts := splitPlatform(cfg.Platform)
		pullOpts.Platforms = []ocispec.Platform{{OS: parts[0], Architecture: parts[1]}}
	}
	if auth := getRegistryAuth(cfg.Image); auth != "" {
		pullOpts.RegistryAuth = auth
	}
	reader, err := m.cli.ImagePull(ctx, cfg.Image, pullOpts)
	if err != nil {
		return fmt.Errorf("pulling image %s: %w", cfg.Image, err)
	}
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()
	return nil
}

func (m *Manager) imageExists(ctx context.Context, imageName string) (bool, error) {
	_, err := m.cli.ImageInspect(ctx, imageName)
	if err == nil {
		return true, nil
	}
	if cerrdefs.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

// ImageExists checks whether a Docker image is available locally.
func (m *Manager) ImageExists(imageName string) bool {
	exists, _ := m.imageExists(context.Background(), imageName)
	return exists
}

func (m *Manager) streamLogs(ctx context.Context, name, containerID string) {
	reader, err := m.cli.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return
	}
	defer func() { _ = reader.Close() }()

	// Docker non-TTY logs use a multiplexed stream: each frame has an 8-byte
	// header (stream type + payload length) followed by the payload. stdcopy
	// demultiplexes frames correctly across buffer boundaries and separates
	// stdout from stderr; LineBuffer then reassembles complete lines before
	// they reach OnOutput.
	emit := func(line string) {
		if m.OnOutput != nil {
			m.OnOutput(name, line)
		}
	}
	stdoutLB := logging.NewLineBuffer(emit)
	stderrLB := logging.NewLineBuffer(emit)
	_, _ = stdcopy.StdCopy(stdoutLB, stderrLB, reader)
	stdoutLB.Flush()
	stderrLB.Flush()
}

// Stop stops and removes a container along with any sidecars labelled as its
// children. Sidecar removal failures are logged but do not fail the parent
// stop — leaving a stale UI container is preferable to surfacing a stop error
// to lifecycle pollers.
func (m *Manager) Stop(ctx context.Context, name string) error {
	m.stopSidecars(ctx, name)
	containerName := m.ContainerName(name)
	timeout := 10
	m.narrate(name, fmt.Sprintf("stopping container %s (timeout %ds)", containerName, timeout))
	if _, err := m.cli.ContainerStop(ctx, containerName, client.ContainerStopOptions{Timeout: &timeout}); err != nil && !cerrdefs.IsNotFound(err) {
		m.narrate(name, "stop error: "+err.Error())
	}
	if _, err := m.cli.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{Force: true}); err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		m.narrate(name, "ERROR: "+err.Error())
		return err
	}
	m.narrate(name, "removed container "+containerName)
	return nil
}

// stopSidecars removes any containers labelled as sidecars of the given parent
// service in this namespace.
func (m *Manager) stopSidecars(ctx context.Context, parentName string) {
	list, err := m.cli.ContainerList(ctx, client.ContainerListOptions{
		All: true,
		Filters: make(client.Filters).
			Add("label", labelManaged+"=true").
			Add("label", labelParent+"="+parentName),
	})
	if err != nil {
		m.narrate(parentName, "sidecar list error: "+err.Error())
		return
	}
	timeout := 10
	for i := range list.Items {
		if !m.matchesNamespace(list.Items[i].Labels) {
			continue
		}
		scName := list.Items[i].Labels[labelService]
		m.narrate(parentName, "stopping sidecar "+scName)
		_, _ = m.cli.ContainerStop(ctx, list.Items[i].ID, client.ContainerStopOptions{Timeout: &timeout})
		if _, err := m.cli.ContainerRemove(ctx, list.Items[i].ID, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
			m.narrate(parentName, "sidecar remove error: "+err.Error())
		}
	}
}

// StopAll stops all orbit-managed containers in this Manager's namespace.
func (m *Manager) StopAll(ctx context.Context) error {
	list, err := m.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", labelManaged+"=true"),
	})
	if err != nil {
		return err
	}
	for i := range list.Items {
		if !m.matchesNamespace(list.Items[i].Labels) {
			continue
		}
		timeout := 10
		_, _ = m.cli.ContainerStop(ctx, list.Items[i].ID, client.ContainerStopOptions{Timeout: &timeout})
		_, _ = m.cli.ContainerRemove(ctx, list.Items[i].ID, client.ContainerRemoveOptions{Force: true})
	}
	return nil
}

// matchesNamespace reports whether a container's labels place it in this
// Manager's namespace. Docker's container list filter doesn't support
// negative label matching, so namespace membership is enforced in Go.
func (m *Manager) matchesNamespace(labels map[string]string) bool {
	return labels[labelNamespace] == m.namespace
}

// StartSidecar starts a sidecar container.
func (m *Manager) StartSidecar(ctx context.Context, parentName string, parent *config.Container, sidecar *config.Sidecar) error {
	pullPolicy := sidecar.PullPolicy
	if pullPolicy == "" {
		pullPolicy = parent.PullPolicy
	}
	sc := &config.Container{
		Name:        sidecar.Name,
		Image:       sidecar.Image,
		PullPolicy:  pullPolicy,
		Platform:    parent.Platform,
		Ports:       sidecar.Ports,
		Environment: sidecar.Environment,
		Volumes:     sidecar.Volumes,
	}
	return m.start(ctx, parentName+"-"+sidecar.Name, sc, parentName)
}

// InspectState returns the current Docker state of a container.
func (m *Manager) InspectState(ctx context.Context, name string) (string, bool, error) {
	containerName := m.ContainerName(name)
	inspect, err := m.cli.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	if err != nil {
		return "", false, err
	}
	return string(inspect.Container.State.Status), inspect.Container.State.Running, nil
}

// Close closes the Docker client.
func (m *Manager) Close() error {
	return m.cli.Close()
}

// CheckImagePull verifies the caller has permission to pull the given image
// from its registry without downloading layers. Uses DistributionInspect which
// is a HEAD-equivalent on the registry manifest.
func (m *Manager) CheckImagePull(ctx context.Context, imageName string) error {
	auth := getRegistryAuth(imageName)
	_, err := m.cli.DistributionInspect(ctx, imageName, client.DistributionInspectOptions{
		EncodedRegistryAuth: auth,
	})
	return err
}

// getRegistryAuth reads Docker CLI credentials from ~/.docker/config.json
// for the registry of the given image.
func getRegistryAuth(imageName string) string {
	// Extract registry from image name
	reg := "https://index.docker.io/v1/" // default
	parts := strings.SplitN(imageName, "/", 2)
	if len(parts) == 2 && strings.Contains(parts[0], ".") {
		reg = parts[0]
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
	if err != nil {
		return ""
	}

	var dockerCfg dockerConfigJSON
	if err := json.Unmarshal(data, &dockerCfg); err != nil {
		return ""
	}

	if cred := lookupCredHelper(dockerCfg.CredHelpers, reg); cred != "" {
		return cred
	}
	return lookupInlineAuth(dockerCfg, reg)
}

type dockerConfigJSON struct {
	Auths       map[string]json.RawMessage `json:"auths"`
	CredHelpers map[string]string          `json:"credHelpers"`
	CredsStore  string                     `json:"credsStore"`
}

func lookupCredHelper(credHelpers map[string]string, reg string) string {
	for key, helper := range credHelpers {
		if authKeyMatches(key, reg) {
			return getCredFromHelper(helper, key)
		}
	}
	return ""
}

func lookupInlineAuth(dockerCfg dockerConfigJSON, reg string) string {
	for key, raw := range dockerCfg.Auths {
		if !authKeyMatches(key, reg) {
			continue
		}
		var authEntry struct {
			Auth string `json:"auth"`
		}
		if json.Unmarshal(raw, &authEntry) == nil && authEntry.Auth != "" {
			// Docker CLI stores base64(username:password); the Engine API
			// expects decoded username/password (raw `auth` is ignored by
			// some registries, notably ECR).
			decoded, err := base64.StdEncoding.DecodeString(authEntry.Auth)
			if err != nil {
				continue
			}
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) != 2 {
				continue
			}
			authJSON, _ := json.Marshal(map[string]string{
				"username":      parts[0],
				"password":      parts[1],
				"serveraddress": reg,
			})
			return base64.URLEncoding.EncodeToString(authJSON)
		}
		// Docker Desktop model: the auths entry exists but is empty; every
		// registry's creds live in the top-level credsStore.
		if dockerCfg.CredsStore != "" {
			return getCredFromHelper(dockerCfg.CredsStore, key)
		}
	}
	return ""
}

// authKeyMatches compares a config.json registry key against the bare registry
// hostname we derived from the image. Docker stores keys in several shapes
// depending on CLI version and prior login flags — with or without https://,
// occasionally with a trailing /v1/ or /v2/ path.
func authKeyMatches(key, reg string) bool {
	if key == reg {
		return true
	}
	trimmed := strings.TrimPrefix(key, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	trimmed = strings.TrimRight(trimmed, "/")
	if i := strings.Index(trimmed, "/"); i >= 0 {
		trimmed = trimmed[:i]
	}
	return trimmed == reg
}

func getCredFromHelper(helper, registry string) string {
	// Execute docker-credential-<helper> get
	cmd := fmt.Sprintf("docker-credential-%s", helper)
	proc := newCredHelperCmd(cmd, registry)
	out, err := proc.Output()
	if err != nil {
		return ""
	}

	var cred struct {
		Username string `json:"Username"`
		Secret   string `json:"Secret"`
	}
	if json.Unmarshal(out, &cred) != nil {
		return ""
	}

	authJSON, _ := json.Marshal(map[string]string{
		"username": cred.Username,
		"password": cred.Secret,
	})
	return base64.URLEncoding.EncodeToString(authJSON)
}

func splitPlatform(p string) [2]string {
	for i, c := range p {
		if c == '/' {
			return [2]string{p[:i], p[i+1:]}
		}
	}
	return [2]string{"linux", p}
}
