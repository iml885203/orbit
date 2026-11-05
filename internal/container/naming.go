package container

// ContainerName returns the Docker container name for a service in the given
// namespace. Empty namespace keeps the legacy "orbit-<svc>" format so existing
// containers and deployments stay compatible.
func ContainerName(namespace, svc string) string {
	if namespace != "" {
		return "orbit-" + namespace + "-" + svc
	}
	return "orbit-" + svc
}

// labelNamespace tags a container with the orbit namespace it belongs to.
const labelNamespace = "orbit.namespace"

// labelParent tags a sidecar container with the orbit service name of its
// parent container, so stopping the parent can find and stop its sidecars.
const labelParent = "orbit.parent"
