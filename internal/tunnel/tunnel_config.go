package tunnel

import (
	"fmt"
	"strings"

	"github.com/iml885203/orbit/config"
	"gopkg.in/yaml.v3"
)

// ClaimConfig configures Orbit's embedded Tunlease client. Token is optional.
type ClaimConfig struct {
	Gateway  string `yaml:"gateway"`
	Token    string `yaml:"token,omitempty"`
	Insecure bool   `yaml:"insecure,omitempty"`
}

// Package init is the guaranteed-early registration spot: config.Load
// runs on CLI startup paths and per daemon request, both binaries link
// this package, and the section must be registered before ANY Load.
func init() {
	config.RegisterExtensionSection("claim", config.ExtensionSection{Decode: decodeClaimSection, Default: sharedClaimDefault})
}

// ClaimFrom extracts the env's claim config, if any.
func ClaimFrom(cfg *config.Config) *ClaimConfig {
	c, _ := cfg.Extension("claim").(*ClaimConfig)
	return c
}
func decodeClaimSection(node *yaml.Node, _ string) (any, error) {
	var c ClaimConfig
	// DecodeStrict, not node.Decode: plain decoding silently drops
	// unknown keys, and stale legacy fields must fail loud so envs
	// migrate their claim sections.
	if err := config.DecodeStrict(node, &c); err != nil {
		return nil, err
	}
	if err := validateClaim(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// sharedClaimDefault loads the shared envs/data/claim.yaml convention.
// Every error path deliberately returns (nil, nil): claim support is
// optional, and an absent or unreadable shared file simply means "this
// env has no tunnel support" — not a config failure.
func sharedClaimDefault(cfgPath string) (any, error) {
	var c ClaimConfig
	found, err := config.LoadSharedSiblingYAML(cfgPath, "claim.yaml", &c)
	if !found || err != nil {
		return nil, nil // absent or unreadable/malformed → no tunnel support
	}
	if err := validateClaim(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
func validateClaim(c *ClaimConfig) error {
	if strings.TrimSpace(c.Gateway) == "" {
		return fmt.Errorf("claim.gateway is required when claim section is present")
	}
	return nil
}
