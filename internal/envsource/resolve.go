package envsource

import (
	"path/filepath"
	"strings"
)

type ManagedTarget struct {
	Path      string
	Identity  string
	Source    Source
	Workspace string
}

func (r *Registry) ResolveManagedEnvironment(orbitHome, requested string) (ManagedTarget, bool, error) {
	if len(r.List()) == 0 || filepath.IsAbs(requested) || strings.HasPrefix(requested, ".") || strings.Contains(requested, `\`) {
		return ManagedTarget{}, false, nil
	}
	var source Source
	var environment string
	var err error
	if strings.Contains(requested, "/") {
		var sourceName string
		sourceName, environment, err = ParseIdentity(requested)
		if err == nil {
			source, err = r.Get(sourceName)
		}
	} else {
		source, err = r.First()
		environment = strings.TrimSuffix(requested, filepath.Ext(requested))
	}
	if err != nil {
		return ManagedTarget{}, true, err
	}
	return ManagedTarget{
		Path:     filepath.Join(EnvsDir(orbitHome, source.Name), environment+".yaml"),
		Identity: Identity(source.Name, environment), Source: source, Workspace: source.Workspace,
	}, true, nil
}
