package tunnel

import (
	"gopkg.in/yaml.v3"
	"testing"
)

func TestDecodeClaimSection(t *testing.T) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("gateway: https://tunlease.example\ntoken: secret\n"), &node); err != nil {
		t.Fatal(err)
	}
	got, err := decodeClaimSection(node.Content[0], "")
	if err != nil {
		t.Fatal(err)
	}
	c := got.(*ClaimConfig)
	if c.Gateway != "https://tunlease.example" || c.Token != "secret" {
		t.Fatalf("claim = %#v", c)
	}
}

func TestDecodeClaimSectionRequiresGateway(t *testing.T) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("token: secret\n"), &node); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeClaimSection(node.Content[0], ""); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDecodeClaimSectionRejectsOldFields(t *testing.T) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("gateway: https://tunlease.example\ncluster_arn: old\n"), &node); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeClaimSection(node.Content[0], ""); err == nil {
		t.Fatal("expected unknown-field error")
	}
}
