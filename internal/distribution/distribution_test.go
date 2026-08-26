package distribution

import "testing"

func TestOfficialAndExportShareDistribution(t *testing.T) {
	d := Official()
	m, err := Export()
	if err != nil {
		t.Fatal(err)
	}
	if m.Schema != Schema || m.EnvironmentRepo != d.EnvRepoURL || m.EnvironmentRef != d.EnvRepoRef || m.InstallURL != d.InstallURL || m.ReleaseAPIURL != d.ReleaseAPIURL || m.DefaultEnvironment != d.DefaultEnv {
		t.Fatalf("export does not match official distribution: %#v %#v", m, d)
	}
	if m.ReleaseRepository != "iml885203/orbit" {
		t.Fatalf("release_repository = %q", m.ReleaseRepository)
	}
}

func TestReleaseRepositoryRejectsUnsupportedURLs(t *testing.T) {
	for _, value := range []string{"", "http://api.github.com/repos/a/b/releases/latest", "https://example.com/repos/a/b/releases/latest", "https://api.github.com/repos/a/b/releases"} {
		if _, err := releaseRepository(value); err == nil {
			t.Fatalf("releaseRepository(%q) succeeded", value)
		}
	}
}
