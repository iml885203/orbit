package container

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	volumetypes "github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

func TestNamespaceVolumeBindsIsolatesNamedVolumesOnly(t *testing.T) {
	binds := []string{"data:/var/lib/data", "/host/config:/etc/config", "./src:/app", `C:\work\config:/windows:ro`, "C:/work/config:/windows-forward:ro"}
	want := []string{"orbit-instance-a-data:/var/lib/data", "/host/config:/etc/config", "./src:/app", `C:\work\config:/windows:ro`, "C:/work/config:/windows-forward:ro"}
	if got := namespaceVolumeBinds("instance-a", binds); !reflect.DeepEqual(got, want) {
		t.Fatalf("binds = %#v, want %#v", got, want)
	}
	if got := namespaceVolumeBinds("", binds); !reflect.DeepEqual(got, binds) {
		t.Fatalf("default binds changed: %#v", got)
	}
}

func TestNamespaceVolumeOwnershipRequiresBothExactLabels(t *testing.T) {
	exact := volumetypes.Volume{Name: "owned", Labels: map[string]string{
		labelManaged: "true", labelNamespace: "instance-a",
	}}
	if err := validateNamespaceVolume(exact, "instance-a"); err != nil {
		t.Fatal(err)
	}
	if !isOwnedNamespaceVolume(exact, "instance-a") {
		t.Fatal("exactly labelled volume was not owned")
	}

	legacy := volumetypes.Volume{Name: "legacy"}
	if err := validateNamespaceVolume(legacy, "instance-a"); err != nil {
		t.Fatalf("unlabelled legacy volume was rejected: %v", err)
	}
	if isOwnedNamespaceVolume(legacy, "instance-a") {
		t.Fatal("unlabelled legacy volume would be purged")
	}

	for _, item := range []volumetypes.Volume{
		{Name: "namespace-only", Labels: map[string]string{labelNamespace: "instance-a"}},
		{Name: "foreign", Labels: map[string]string{labelManaged: "true", labelNamespace: "instance-b"}},
	} {
		if err := validateNamespaceVolume(item, "instance-a"); err == nil {
			t.Fatalf("conflicting volume %+v was accepted", item)
		}
		if isOwnedNamespaceVolume(item, "instance-a") {
			t.Fatalf("conflicting volume %+v would be purged", item)
		}
	}
}

func TestNetworkNameKeepsDefaultBackwardCompatible(t *testing.T) {
	if got := NetworkName(""); got != "orbit" {
		t.Fatalf("default network = %q", got)
	}
	if got := NetworkName("instance-a"); got != "orbit-instance-a" {
		t.Fatalf("named network = %q", got)
	}
}

func TestEnsureNamespaceVolumesLabelsNewVolumesAndPreservesLegacyVolumes(t *testing.T) {
	var created []client.VolumeCreateOptions
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/orbit-instance-a-new"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes/orbit-instance-a-legacy"):
			_, _ = w.Write([]byte(`{"Name":"orbit-instance-a-legacy","Labels":null}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/volumes/create"):
			var options client.VolumeCreateOptions
			if err := json.NewDecoder(r.Body).Decode(&options); err != nil {
				t.Fatal(err)
			}
			created = append(created, options)
			_ = json.NewEncoder(w).Encode(map[string]any{"Name": options.Name, "Labels": options.Labels})
		default:
			http.Error(w, `{"message":"unexpected Docker API request"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := client.New(
		client.WithHost("tcp://"+endpoint.Host),
		client.WithScheme("http"),
		client.WithHTTPClient(server.Client()),
		client.WithAPIVersion("1.52"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	err = ensureNamespaceVolumes(context.Background(), cli, "instance-a", []string{
		"new:/data", "legacy:/legacy", "/host/path:/host",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("created volumes = %+v, want only the missing volume", created)
	}
	if created[0].Name != "orbit-instance-a-new" || created[0].Labels[labelNamespace] != "instance-a" || created[0].Labels[labelManaged] != "true" {
		t.Fatalf("created volume = %+v", created[0])
	}
}
