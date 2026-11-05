package daemonsrv

import "github.com/iml885203/orbit/internal/config"

func infraIconForContainer(c *config.Container) string {
	if c.ResolveKind() != "infra" {
		return ""
	}
	return c.Icon
}
