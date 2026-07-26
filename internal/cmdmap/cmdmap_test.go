package cmdmap

import (
	"reflect"
	"testing"
)

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		ok      bool
		params  map[string]string
	}{
		{"/api/up", "/api/up", true, map[string]string{}},
		{"/api/restart/:name", "/api/restart/api", true, map[string]string{"name": "api"}},
		{"/api/restart/:name", "/api/restart/", false, nil},
		{"/api/restart/:name", "/api/stop/api", false, nil},
		{"/api/service-mode/:name", "/api/service-mode/catalog", true, map[string]string{"name": "catalog"}},
		{"/api/restart/:name", "/api/restart/api/extra", false, nil},
	}
	for _, c := range cases {
		params, ok := matchPattern(c.pattern, c.path)
		if ok != c.ok {
			t.Fatalf("pattern=%q path=%q: ok=%v want=%v", c.pattern, c.path, ok, c.ok)
		}
		if ok && !reflect.DeepEqual(params, c.params) {
			t.Fatalf("pattern=%q path=%q: params=%v want=%v", c.pattern, c.path, params, c.params)
		}
	}
}

func TestResolveFirstMatch(t *testing.T) {
	defer setRulesForTest([]Rule{
		{Method: "POST", Pattern: "/api/up", Build: func(_ map[string]string, _ []byte) Entry {
			return Entry{Command: "orbit up", Summary: "start everything", HasCLI: true, UserAction: true}
		}},
		{Method: "POST", Pattern: "/api/restart/:name", Build: func(p map[string]string, _ []byte) Entry {
			return Entry{Command: "orbit restart " + p["name"], Summary: "restart service " + p["name"], HasCLI: true, UserAction: true}
		}},
	})()

	got := Resolve("POST", "/api/up", nil)
	if got.Command != "orbit up" || !got.HasCLI || !got.UserAction {
		t.Fatalf("up: got %+v", got)
	}

	got = Resolve("POST", "/api/restart/api", nil)
	if got.Command != "orbit restart api" {
		t.Fatalf("restart: got %+v", got)
	}
	if got.PathPattern != "/api/restart/:name" {
		t.Fatalf("PathPattern: got %q", got.PathPattern)
	}

	got = Resolve("GET", "/api/unknown", nil)
	if got.UserAction {
		t.Fatalf("unknown path must default to UserAction=false: %+v", got)
	}
}

func TestDefaultRules(t *testing.T) {
	cases := []struct {
		method, path string
		body         string
		wantCmd      string
		wantHasCLI   bool
		wantUserAct  bool
	}{
		{"POST", "/api/up", "", "orbit up", true, true},
		{"POST", "/api/up", `{"infra_only":true}`, "orbit up --infra", true, true},
		{"POST", "/api/restart/api", "", "orbit restart api", true, true},
		{"POST", "/api/stop/catalog", "", "orbit down catalog", true, true},
		{"PUT", "/api/service-mode/api", `{"mode":"container"}`, "orbit service mode api container", true, true},
		{"PUT", "/api/edges/worker/redis", `{"detached":true}`, "orbit edge detach worker redis", true, true},
		{"PUT", "/api/envs/current", `{"env":"local"}`, "orbit switch local", true, true},
		{"PUT", "/api/env-toggles", "", "orbit env toggle   off", true, true},
		{"PUT", "/api/settings", "", "", false, true},
		{"GET", "/api/doctor", "", "orbit doctor", true, false},
		{"GET", "/api/graph", "", "", false, false},
		{"GET", "/api/version", "", "", false, false},
	}
	for _, c := range cases {
		got := Resolve(c.method, c.path, []byte(c.body))
		if got.Command != c.wantCmd || got.HasCLI != c.wantHasCLI || got.UserAction != c.wantUserAct {
			t.Fatalf("%s %s body=%s\n  got  cmd=%q has=%v ua=%v\n  want cmd=%q has=%v ua=%v",
				c.method, c.path, c.body,
				got.Command, got.HasCLI, got.UserAction,
				c.wantCmd, c.wantHasCLI, c.wantUserAct)
		}
	}
}

func TestEdgesRule_DetachAndAttach(t *testing.T) {
	defer setRulesForTest(defaultRules())()

	got := Resolve("PUT", "/api/edges/payments/worker",
		[]byte(`{"detached":true}`))
	if !got.HasCLI {
		t.Errorf("detach: HasCLI = false")
	}
	if got.Command != "orbit edge detach payments worker" {
		t.Errorf("detach: Command = %q", got.Command)
	}
	if got.Summary != "detach edge payments -> worker" {
		t.Errorf("detach: Summary = %q", got.Summary)
	}

	got = Resolve("PUT", "/api/edges/payments/worker",
		[]byte(`{"detached":false}`))
	if got.Command != "orbit edge attach payments worker" {
		t.Errorf("attach: Command = %q", got.Command)
	}
	if got.Summary != "attach edge payments -> worker" {
		t.Errorf("attach: Summary = %q", got.Summary)
	}
}

func TestServiceModeRule_HasCLI(t *testing.T) {
	defer setRulesForTest(defaultRules())()
	got := Resolve("PUT", "/api/service-mode/worker",
		[]byte(`{"mode":"container"}`))
	if !got.HasCLI || got.Command != "orbit service mode worker container" {
		t.Errorf("got %+v", got)
	}
}

func TestEnvTogglesRule_HasCLI(t *testing.T) {
	defer setRulesForTest(defaultRules())()
	got := Resolve("PUT", "/api/env-toggles",
		[]byte(`{"service":"api","var":"FEATURE_X","enabled":true}`))
	if !got.HasCLI {
		t.Errorf("HasCLI = false")
	}
	if got.Command != "orbit env toggle api FEATURE_X on" {
		t.Errorf("Command = %q", got.Command)
	}
	got = Resolve("PUT", "/api/env-toggles",
		[]byte(`{"service":"api","var":"FEATURE_X","enabled":false}`))
	if got.Command != "orbit env toggle api FEATURE_X off" {
		t.Errorf("off: Command = %q", got.Command)
	}
}

func TestSettingsRule_SingleKey(t *testing.T) {
	defer setRulesForTest(defaultRules())()
	got := Resolve("PUT", "/api/settings",
		[]byte(`{"show_history":true}`))
	if !got.HasCLI {
		t.Errorf("HasCLI = false")
	}
	if got.Command != "orbit settings set show-history true" {
		t.Errorf("Command = %q", got.Command)
	}
}

func TestSettingsRule_MultiKeyChained(t *testing.T) {
	defer setRulesForTest(defaultRules())()
	got := Resolve("PUT", "/api/settings",
		[]byte(`{"sql_server_image":"example.db:latest","sql_server_pull_policy":"never"}`))
	// Order is deterministic (sorted by key). sql_server_image < sql_server_pull_policy.
	want := "orbit settings set sql-server-image example.db:latest && orbit settings set sql-server-pull-policy never"
	if got.Command != want {
		t.Errorf("Command = %q\nwant %q", got.Command, want)
	}
}

func TestServiceEnvRule_Removed(t *testing.T) {
	defer setRulesForTest(defaultRules())()
	got := Resolve("PUT", "/api/service-env/api", []byte(`{}`))
	if got.UserAction {
		t.Errorf("expected no user-action rule for /api/service-env, got %+v", got)
	}
}
