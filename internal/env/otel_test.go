package env

import "testing"

func TestInjectOTEL(t *testing.T) {
	m := map[string]string{"EXISTING": "1"}
	InjectOTEL(m, "api", "http://localhost:4318")

	want := map[string]string{
		"OTEL_SERVICE_NAME":           "api",
		"OTEL_TRACES_EXPORTER":        "otlp",
		"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318",
		"OTEL_TRACES_SAMPLER":         "always_on",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
	if m["EXISTING"] != "1" {
		t.Error("InjectOTEL clobbered pre-existing env")
	}
}

// A service that already points at its own collector must be left entirely
// alone (hybrid stand-aside).
func TestInjectOTEL_StandsAsideForExistingEndpoint(t *testing.T) {
	m := map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector.internal:4318",
		"OTEL_SERVICE_NAME":           "api-custom",
	}
	InjectOTEL(m, "api", "http://localhost:4318")
	if m["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://collector.internal:4318" {
		t.Errorf("endpoint overwritten: %q", m["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
	if m["OTEL_SERVICE_NAME"] != "api-custom" {
		t.Errorf("service name overwritten: %q", m["OTEL_SERVICE_NAME"])
	}
	if _, set := m["OTEL_TRACES_SAMPLER"]; set {
		t.Error("InjectOTEL added vars despite standing aside")
	}
}
