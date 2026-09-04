package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func (s *Service) ResolveURL() string {
	if s == nil {
		return ""
	}
	return resolveApplicationURL(s.URL, s.Ports)
}

func (c *Container) ResolveURL() string {
	if c == nil {
		return ""
	}
	return resolveApplicationURL(c.URL, c.Ports)
}

func resolveApplicationURL(declared string, ports map[string]PortDef) string {
	if strings.TrimSpace(declared) == "" {
		if port, ok := ports["http"]; ok {
			return fmt.Sprintf("http://localhost:%d", port.Host)
		}
		if port, ok := ports["https"]; ok {
			return fmt.Sprintf("https://localhost:%d", port.Host)
		}
		return ""
	}
	return declared
}

func RemapApplicationURL(rawURL string, declaredPort, resolvedPort int) string {
	if rawURL == "" || declaredPort == resolvedPort {
		return rawURL
	}
	endpoint, err := url.Parse(rawURL)
	if err != nil || !isLoopbackEndpoint(endpoint) || applicationURLPort(endpoint) != declaredPort {
		return rawURL
	}
	endpoint.Host = net.JoinHostPort(endpoint.Hostname(), strconv.Itoa(resolvedPort))
	return endpoint.String()
}

func validateApplicationURL(kind, name, rawURL string, ports map[string]PortDef) error {
	if strings.TrimSpace(rawURL) == "" {
		return nil
	}
	endpoint, err := url.Parse(rawURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return fmt.Errorf("%s %q url must be an absolute http or https URL", kind, name)
	}
	if !isLoopbackEndpoint(endpoint) {
		return nil
	}
	urlPort := applicationURLPort(endpoint)
	for _, port := range ports {
		if port.Host == urlPort {
			return nil
		}
	}
	if port, ok := ports[endpoint.Scheme]; ok {
		return fmt.Errorf(
			"%s %q url uses port %d but ports.%s declares %d",
			kind,
			name,
			urlPort,
			endpoint.Scheme,
			port.Host,
		)
	}
	return nil
}

func isLoopbackEndpoint(endpoint *url.URL) bool {
	host := endpoint.Hostname()
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func applicationURLPort(endpoint *url.URL) int {
	if port, err := strconv.Atoi(endpoint.Port()); err == nil {
		return port
	}
	if endpoint.Scheme == "http" {
		return 80
	}
	if endpoint.Scheme == "https" {
		return 443
	}
	return 0
}
