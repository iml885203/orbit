package devdb

import "testing"

// The publish target's env resource name must survive `--instance`, where the
// container carries an instance prefix the resource does not. Reported from a
// live run: `up` said healthy, publish said the same target was unavailable,
// and its hint named a resource `up` then rejected as unknown.
func TestTargetServiceName(t *testing.T) {
	for _, tc := range []struct {
		name          string
		metaService   string
		containerName string
		want          string
	}{
		{
			name:          "instance mode keeps the resource name",
			metaService:   "sql-server",
			containerName: "orbit-instance-v15-test-8e4a09e5-sql-server",
			want:          "sql-server",
		},
		{
			name:          "shared mode keeps the resource name",
			metaService:   "sql-server",
			containerName: "orbit-sql-server",
			want:          "sql-server",
		},
		{
			name:          "namespaced container keeps the resource name",
			metaService:   "database",
			containerName: "orbit-team-database",
			want:          "database",
		},
		{
			name:          "older daemon without the field falls back",
			metaService:   "",
			containerName: "orbit-sql-server",
			want:          "sql-server",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := targetServiceName(tc.metaService, tc.containerName); got != tc.want {
				t.Errorf("targetServiceName(%q, %q) = %q, want %q",
					tc.metaService, tc.containerName, got, tc.want)
			}
		})
	}
}
