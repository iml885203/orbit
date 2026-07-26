package daemon

import "github.com/iml885203/orbit/config"

func infraIconForContainer(c *config.Container) string {
	if c.ResolveKind() != "infra" {
		return ""
	}
	return c.Icon
}
