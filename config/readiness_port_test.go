package config

import "testing"

// An implicit readiness probe is TCP, so it needs an address and nothing more.
// "https" therefore identifies an endpoint just as well as "http" — a resource
// whose only endpoint is TLS used to get no probe at all, and `orbit doctor`
// then warned about a readiness signal the author could not supply.
func TestReadinessPort(t *testing.T) {
	tests := []struct {
		name  string
		ports map[string]PortDef
		want  int
	}{
		{
			name:  "single unnamed endpoint",
			ports: map[string]PortDef{"grpc": {Host: 9090}},
			want:  9090,
		},
		{
			name:  "http wins among several",
			ports: map[string]PortDef{"http": {Host: 8080}, "metrics": {Host: 9100}},
			want:  8080,
		},
		{
			name:  "https identifies the endpoint too",
			ports: map[string]PortDef{"https": {Host: 8443}, "metrics": {Host: 9100}},
			want:  8443,
		},
		{
			name:  "http preferred when both schemes are published",
			ports: map[string]PortDef{"https": {Host: 8443}, "http": {Host: 8080}},
			want:  8080,
		},
		{
			name:  "several endpoints, none of them a scheme alias",
			ports: map[string]PortDef{"grpc": {Host: 9090}, "metrics": {Host: 9100}},
			want:  0,
		},
		{
			name:  "no endpoints",
			ports: nil,
			want:  0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ReadinessPort(test.ports); got != test.want {
				t.Errorf("ReadinessPort = %d, want %d", got, test.want)
			}
		})
	}
}
