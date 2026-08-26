package distribution

import (
	"errors"
	"net/url"
	"strings"

	"github.com/iml885203/orbit/extension"
)

const Schema = "orbit.distribution.v1"

var official = extension.Distribution{
	EnvRepoURL:        "https://github.com/iml885203/orbit-demo.git",
	EnvRepoRef:        "v2026.8.1",
	InstallURL:        "https://raw.githubusercontent.com/iml885203/orbit/main/scripts/install.sh",
	ReleaseAPIURL:     "https://api.github.com/repos/iml885203/orbit/releases/latest",
	ReleaseRepository: "iml885203/orbit",
	DefaultEnv:        "quickstart.yaml",
}

type Metadata struct {
	Schema             string `json:"schema"`
	EnvironmentRepo    string `json:"environment_repository"`
	EnvironmentRef     string `json:"environment_ref"`
	InstallURL         string `json:"install_url"`
	ReleaseAPIURL      string `json:"release_api_url"`
	ReleaseRepository  string `json:"release_repository"`
	DefaultEnvironment string `json:"default_environment"`
}

func Official() extension.Distribution { return official }

func Export() (Metadata, error) {
	repository, err := releaseRepository(official.ReleaseAPIURL)
	if err != nil {
		return Metadata{}, err
	}
	if repository != official.ReleaseRepository {
		return Metadata{}, errors.New("official release repository does not match its API URL")
	}
	metadata := Metadata{
		Schema: Schema, EnvironmentRepo: official.EnvRepoURL,
		EnvironmentRef: official.EnvRepoRef, InstallURL: official.InstallURL,
		ReleaseAPIURL: official.ReleaseAPIURL, ReleaseRepository: official.ReleaseRepository,
		DefaultEnvironment: official.DefaultEnv,
	}
	if metadata.EnvironmentRepo == "" || metadata.EnvironmentRef == "" || metadata.InstallURL == "" || metadata.DefaultEnvironment == "" {
		return Metadata{}, errors.New("official distribution metadata has an empty required field")
	}
	return metadata, nil
}

func releaseRepository(apiURL string) (string, error) {
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "api.github.com" {
		return "", errors.New("official release API URL must use api.github.com HTTPS")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "repos" || parts[3] != "releases" || parts[4] != "latest" || parts[1] == "" || parts[2] == "" {
		return "", errors.New("official release API URL has an unsupported path")
	}
	return parts[1] + "/" + parts[2], nil
}
