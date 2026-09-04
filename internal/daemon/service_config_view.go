// Pure projections from a config snapshot to wire-facing views, shared by
// the status endpoint (handlers_service.go), the SSE status stream
// (handlers_events.go) and the mode handler. They take the caller's
// snapshot so every view in one response renders one config generation.

package daemon

import (
	"fmt"

	"github.com/iml885203/orbit/config"
)

// getContainerImage returns the configured image for a container service,
// or "" if the service is not a container or is not defined in config.
func getContainerImage(cfg *config.Config, name, kind string) string {
	if kind != "container" {
		return ""
	}
	if c, ok := cfg.Containers[name]; ok {
		return c.Image
	}
	return ""
}

// getResourceRole returns the configured user-journey role for a resource.
func getResourceRole(cfg *config.Config, name, kind string) string {
	if kind == "container" {
		if c, ok := cfg.Containers[name]; ok {
			return c.ResolveKind()
		}
		return ""
	}
	if svc, ok := cfg.Services[name]; ok {
		return svc.ResolveKind()
	}
	return ""
}

func getResourceURL(cfg *config.Config, name, kind string) string {
	if kind == "container" {
		if container, ok := cfg.Containers[name]; ok {
			return container.ResolveURL()
		}
		return ""
	}
	if service, ok := cfg.Services[name]; ok {
		return service.ResolveURL()
	}
	return ""
}

// getServicePorts returns host ports for a service/container from config.
func getServicePorts(cfg *config.Config, name, kind string) map[string]int {
	ports := make(map[string]int)
	if kind == "container" {
		if c, ok := cfg.Containers[name]; ok {
			for label, pd := range c.Ports {
				ports[label] = pd.Host
			}
		}
	} else {
		if svc, ok := cfg.Services[name]; ok {
			for label, pd := range svc.Ports {
				ports[label] = pd.Host
			}
		}
	}
	if len(ports) == 0 {
		return nil
	}
	return ports
}

// getSidecarInfos returns UI URLs for sidecars of a container.
func getSidecarInfos(cfg *config.Config, name, kind string) []SidecarInfo {
	if kind != "container" {
		return nil
	}
	c, ok := cfg.Containers[name]
	if !ok || len(c.Sidecars) == 0 {
		return nil
	}
	var infos []SidecarInfo
	for _, sc := range c.Sidecars {
		if pd, ok := sc.Ports["ui"]; ok {
			infos = append(infos, SidecarInfo{
				Name: sc.Name,
				URL:  fmt.Sprintf("http://localhost:%d", pd.Host),
			})
		}
	}
	return infos
}

// isDualDefined returns true if a name exists in both containers and services config.
func isDualDefined(cfg *config.Config, name string) bool {
	_, inContainers := cfg.Containers[name]
	_, inServices := cfg.Services[name]
	return inContainers && inServices
}
