package container

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/iml885203/orbit/dockerctx"
	"github.com/moby/moby/client"
)

func namespaceVolumeBinds(namespace string, binds []string) []string {
	if namespace == "" {
		return binds
	}
	isolated := make([]string, len(binds))
	for i, bind := range binds {
		parts := strings.SplitN(bind, ":", 2)
		if len(parts) != 2 || filepath.IsAbs(parts[0]) || strings.HasPrefix(parts[0], ".") || strings.HasPrefix(parts[0], "~") {
			isolated[i] = bind
			continue
		}
		isolated[i] = "orbit-" + namespace + "-" + parts[0] + ":" + parts[1]
	}
	return isolated
}

// An empty namespace is rejected so cleanup can never target legacy default-runtime resources.
func PurgeNamespace(ctx context.Context, namespace string) error {
	if namespace == "" {
		return fmt.Errorf("refusing to purge the default namespace")
	}
	cli, err := dockerctx.NewClient()
	if err != nil {
		return fmt.Errorf("connecting to Docker: %w", err)
	}
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All: true, Filters: make(client.Filters).Add("label", labelNamespace+"="+namespace),
	})
	if err != nil {
		return fmt.Errorf("listing instance containers: %w", err)
	}
	for _, item := range containers.Items {
		if _, err := cli.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
			return fmt.Errorf("removing instance container %s: %w", item.ID, err)
		}
	}
	volumes, err := cli.VolumeList(ctx, client.VolumeListOptions{Filters: make(client.Filters).Add("name", "orbit-"+namespace+"-")})
	if err != nil {
		return fmt.Errorf("listing instance volumes: %w", err)
	}
	for _, volume := range volumes.Items {
		if !strings.HasPrefix(volume.Name, "orbit-"+namespace+"-") {
			continue
		}
		if _, err := cli.VolumeRemove(ctx, volume.Name, client.VolumeRemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("removing instance volume %s: %w", volume.Name, err)
		}
	}
	networks, err := cli.NetworkList(ctx, client.NetworkListOptions{Filters: make(client.Filters).Add("name", "^"+NetworkName(namespace)+"$")})
	if err != nil {
		return fmt.Errorf("listing instance network: %w", err)
	}
	for _, network := range networks.Items {
		if network.Name != NetworkName(namespace) {
			continue
		}
		if _, err := cli.NetworkRemove(ctx, network.ID, client.NetworkRemoveOptions{}); err != nil {
			return fmt.Errorf("removing instance network: %w", err)
		}
	}
	return nil
}
