package devdb

// Publish-target resolution: which container `db publish` connects to
// (the sql_projects.target declaration, else the sql-server convention)
// and which published host port reaches it.

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/iml885203/orbit/config"
)

func publishTargetIdentity(environmentPath, target, image string) string {
	return strings.Join([]string{filepath.Clean(environmentPath), target, image}, "\x00")
}

func (f *dbFeature) publishTargetID() string {
	c, targetName, ok := f.publishTarget()
	if !ok {
		return ""
	}
	image := c.Image
	if override := f.host.Settings().Get("sql_server_image"); override != "" {
		image = override
	}
	return publishTargetIdentity(f.host.ConfigPath(), dbTargetDockerName(targetName), image)
}

// publishTarget resolves the container publishes go to: the
// sql_projects.target declaration when present, else the sql-server
// convention the rest of the DB workflow uses.
func (f *dbFeature) publishTarget() (*config.Container, string, bool) {
	cfg := f.host.Config()
	if sp := cfg.SQLProjects; sp != nil && sp.Target != "" {
		c, ok := cfg.Containers[sp.Target]
		return c, sp.Target, ok
	}
	c, ok := f.sqlServerContainerConfig()
	return c, SQLServerContainerName, ok
}

// publishTargetHostPort resolves the published port publishes
// connect to: the port whose container-internal target is 1433, else —
// deterministically — the single declared port. Multiple ports with no
// 1433 target is ambiguous and refused (a map iteration pick would
// silently vary between runs).
func publishTargetHostPort(c *config.Container) (int, error) {
	var hosts []int
	for _, p := range c.Ports {
		if p.Target == 1433 {
			return p.Host, nil
		}
		hosts = append(hosts, p.Host)
	}
	if len(hosts) == 1 {
		return hosts[0], nil
	}
	if len(hosts) == 0 {
		return 0, fmt.Errorf("publish target declares no published port")
	}
	return 0, fmt.Errorf("publish target declares %d ports and none targets 1433 — ambiguous", len(hosts))
}
