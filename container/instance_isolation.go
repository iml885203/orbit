package container

import (
	"context"
	"fmt"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/iml885203/orbit/dockerctx"
	orbitvolume "github.com/iml885203/orbit/volume"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

func namespaceVolumeBinds(namespace string, binds []string) []string {
	if namespace == "" {
		return binds
	}
	isolated := make([]string, len(binds))
	for i, bind := range binds {
		source, suffix := orbitvolume.SplitShort(bind)
		if suffix == "" || orbitvolume.IsBindSource(source) {
			isolated[i] = bind
			continue
		}
		isolated[i] = "orbit-" + namespace + "-" + source + suffix
	}
	return isolated
}

func ensureNamespaceVolumes(ctx context.Context, cli *client.Client, namespace string, binds []string) error {
	if namespace == "" {
		return nil
	}
	for _, bind := range namespaceVolumeBinds(namespace, binds) {
		source, suffix := orbitvolume.SplitShort(bind)
		if suffix == "" || !strings.HasPrefix(source, "orbit-"+namespace+"-") {
			continue
		}
		if result, err := cli.VolumeInspect(ctx, source, client.VolumeInspectOptions{}); err == nil {
			if err := validateNamespaceVolume(result.Volume, namespace); err != nil {
				return err
			}
			continue
		} else if !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("inspecting instance volume %s: %w", source, err)
		}
		result, err := cli.VolumeCreate(ctx, client.VolumeCreateOptions{
			Name: source,
			Labels: map[string]string{
				labelManaged:   "true",
				labelNamespace: namespace,
			},
		})
		if err != nil {
			return fmt.Errorf("creating instance volume %s: %w", source, err)
		}
		if err := validateNamespaceVolume(result.Volume, namespace); err != nil {
			return err
		}
	}
	return nil
}

func validateNamespaceVolume(item volume.Volume, namespace string) error {
	if len(item.Labels) == 0 {
		return nil
	}
	if item.Labels[labelManaged] == "true" && item.Labels[labelNamespace] == namespace {
		return nil
	}
	return fmt.Errorf("volume %s has conflicting ownership labels", item.Name)
}

func isOwnedNamespaceVolume(item volume.Volume, namespace string) bool {
	return item.Labels[labelManaged] == "true" && item.Labels[labelNamespace] == namespace
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
	defer func() { _ = cli.Close() }()
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All: true, Filters: make(client.Filters).
			Add("label", labelManaged+"=true").
			Add("label", labelNamespace+"="+namespace),
	})
	if err != nil {
		return fmt.Errorf("listing instance containers: %w", err)
	}
	for _, item := range containers.Items {
		if _, err := cli.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
			return fmt.Errorf("removing instance container %s: %w", item.ID, err)
		}
	}
	volumes, err := cli.VolumeList(ctx, client.VolumeListOptions{
		Filters: make(client.Filters).
			Add("label", labelManaged+"=true").
			Add("label", labelNamespace+"="+namespace),
	})
	if err != nil {
		return fmt.Errorf("listing instance volumes: %w", err)
	}
	for _, volume := range volumes.Items {
		if !isOwnedNamespaceVolume(volume, namespace) {
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
