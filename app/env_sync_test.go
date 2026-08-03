package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/iml885203/orbit/cli"
	"github.com/iml885203/orbit/extension"
	"github.com/iml885203/orbit/internal/envsync"
)

// setTestDistribution swaps the package distribution for one test.
func setTestDistribution(t *testing.T, d extension.Distribution) {
	t.Helper()
	prev := distribution
	distribution = d
	t.Cleanup(func() { distribution = prev })
}

func TestResolveEnvRepositoryKeepsURLAndRefFromOnePrecedenceLevel(t *testing.T) {
	setTestDistribution(t, extension.Distribution{
		EnvRepoURL: "https://example.com/default.git",
		EnvRepoRef: "v1.2.3",
	})
	tests := []struct {
		name       string
		flagURL    string
		flagRef    string
		settingURL string
		settingRef string
		envURL     string
		envRef     string
		want       envRepository
	}{
		{
			name:    "explicit URL does not inherit another source ref",
			flagURL: "https://example.com/explicit.git",
			want:    envRepository{URL: "https://example.com/explicit.git"},
		},
		{
			name:       "explicit ref updates saved repository",
			flagRef:    "release",
			settingURL: "https://example.com/saved.git",
			settingRef: "old",
			want:       envRepository{URL: "https://example.com/saved.git", Ref: "release"},
		},
		{
			name:   "environment pair stays together",
			envURL: "https://example.com/environment.git",
			envRef: "environment-ref",
			want:   envRepository{URL: "https://example.com/environment.git", Ref: "environment-ref"},
		},
		{
			name: "distribution pair stays together",
			want: envRepository{URL: "https://example.com/default.git", Ref: "v1.2.3"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("ORBIT_ENV_REPO_URL", test.envURL)
			t.Setenv("ORBIT_ENV_REPO_REF", test.envRef)
			if got := resolveEnvRepository(test.flagURL, test.flagRef, test.settingURL, test.settingRef); got != test.want {
				t.Fatalf("repository = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestEnvRepoSyncErrorIdentifiesPrivateRepoRemedy(t *testing.T) {
	source := &envsync.CloneError{
		URL: "https://github.com/example/private-env.git",
		Err: errors.New("exit status 128"),
	}
	err := envRepoSyncError(source)
	if !errors.Is(err, cli.ErrEnvRepoAccess) {
		t.Fatalf("error = %v, want ErrEnvRepoAccess", err)
	}
	for _, evidence := range []string{
		source.URL,
		"gh auth login",
		"gh auth setup-git",
		"orbit source sync",
	} {
		if !strings.Contains(err.Error(), evidence) {
			t.Errorf("error missing %q: %v", evidence, err)
		}
	}
}

func TestEnvRepoSyncErrorDoesNotAssumeMissingGitHubRepoIsPrivate(t *testing.T) {
	source := &envsync.CloneError{
		URL:    "https://github.com/example/typo-env.git",
		Err:    errors.New("exit status 128"),
		Output: "remote: Repository not found.\nfatal: repository not found",
	}
	err := envRepoSyncError(source)
	if !errors.Is(err, cli.ErrEnvRepoUnavailable) {
		t.Fatalf("error = %v, want ErrEnvRepoUnavailable", err)
	}
	for _, evidence := range []string{
		"Check the GitHub owner and repository name first",
		"same response when a private repository is hidden",
	} {
		if !strings.Contains(err.Error(), evidence) {
			t.Errorf("error missing %q: %v", evidence, err)
		}
	}

	var output bytes.Buffer
	if writeErr := cli.WriteJSONError(&output, "orbit env sync --json", err); writeErr != nil {
		t.Fatal(writeErr)
	}
	envelope := decodeEnvelope(t, output.Bytes())
	if envelope.Error == nil || envelope.Error.Code != "env_repo_unavailable" {
		t.Fatalf("error envelope = %+v", envelope)
	}
	if len(envelope.RecommendedActions) != 0 {
		t.Fatalf("ambiguous failure recommends an assumptive action: %+v", envelope.RecommendedActions)
	}
}
