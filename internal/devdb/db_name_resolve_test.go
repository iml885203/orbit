package devdb

import (
	"strings"
	"testing"
)

func sampleProjects() []DevDBProject {
	return []DevDBProject{
		{Name: "dbproject.info", Path: "/w/dbproject.info", Databases: []string{"ApplicationSetting"}},
		{Name: "dbproject.game", Path: "/w/dbproject.game", Databases: []string{"OrdersDB", "GameInfoDB", "GameSummaryDB"}},
		{Name: "dbproject.platform", Path: "/w/dbproject.platform", Databases: []string{"PlatformDB"}},
	}
}

func TestResolveDBArg_ExactDatabaseName(t *testing.T) {
	r, err := resolveDBArg(sampleProjects(), "GameInfoDB")
	if err != nil {
		t.Fatal(err)
	}
	if r.FromProject() {
		t.Error("a database name must not resolve as a project")
	}
	if len(r.DBs) != 1 || r.DBs[0] != "GameInfoDB" {
		t.Errorf("want [GameInfoDB]; got %v", r.DBs)
	}
}

func TestResolveDBArg_SingleDatabaseProject(t *testing.T) {
	r, err := resolveDBArg(sampleProjects(), "dbproject.info")
	if err != nil {
		t.Fatal(err)
	}
	if !r.FromProject() || r.Project != "dbproject.info" {
		t.Errorf("want project dbproject.info; got %+v", r)
	}
	if len(r.DBs) != 1 || r.DBs[0] != "ApplicationSetting" {
		t.Errorf("single-db project must resolve to its one db; got %v", r.DBs)
	}
}

func TestResolveDBArg_MultiDatabaseProjectExpands(t *testing.T) {
	r, err := resolveDBArg(sampleProjects(), "dbproject.game")
	if err != nil {
		t.Fatal(err)
	}
	if !r.FromProject() || len(r.DBs) != 3 {
		t.Errorf("want 3 dbs from dbproject.game; got %+v", r)
	}
}

func TestResolveDBArg_ProjectNameCaseInsensitive(t *testing.T) {
	r, err := resolveDBArg(sampleProjects(), "DBPROJECT.GAME")
	if err != nil {
		t.Fatal(err)
	}
	if !r.FromProject() || len(r.DBs) != 3 {
		t.Errorf("project match must be case-insensitive; got %+v", r)
	}
}

func TestResolveDBArg_UnknownSuggestsClosest(t *testing.T) {
	_, err := resolveDBArg(sampleProjects(), "GameInfDB") // one deletion from GameInfoDB
	if err == nil {
		t.Fatal("expected an error for an unknown name")
	}
	if !strings.Contains(err.Error(), "GameInfoDB") {
		t.Errorf("error should suggest the closest name; got %q", err.Error())
	}
}

func TestResolveSingleDBArg_RejectsMultiDatabaseProject(t *testing.T) {
	_, err := resolveSingleDBArg(sampleProjects(), "dbproject.game")
	if err == nil {
		t.Fatal("a multi-db project must be rejected for a single-db command")
	}
	if !strings.Contains(err.Error(), "OrdersDB") {
		t.Errorf("rejection should list the databases to pick from; got %q", err.Error())
	}
}

func TestResolveSingleDBArg_AllowsSingleDatabaseProject(t *testing.T) {
	db, err := resolveSingleDBArg(sampleProjects(), "dbproject.platform")
	if err != nil {
		t.Fatal(err)
	}
	if db != "PlatformDB" {
		t.Errorf("want PlatformDB; got %q", db)
	}
}

func TestResolveSingleDBArg_AllowsDatabaseName(t *testing.T) {
	db, err := resolveSingleDBArg(sampleProjects(), "OrdersDB")
	if err != nil {
		t.Fatal(err)
	}
	if db != "OrdersDB" {
		t.Errorf("want OrdersDB; got %q", db)
	}
}
