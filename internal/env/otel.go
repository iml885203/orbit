package env

// InjectOTEL points a dev service at Orbit's built-in OTLP/HTTP receiver by
// setting the standard OTEL_* exporter env vars, mutating env in place.
//
// Hybrid policy (Aspire-aligned): a service that already sets
// OTEL_EXPORTER_OTLP_ENDPOINT is deliberately wired to its own collector, so
// InjectOTEL leaves ALL OTEL_* vars untouched — overwriting them would silently
// redirect that service's telemetry to Orbit. A service with no endpoint of its
// own gets the full set.
//
// Services that register a parameterless OTLP exporter read these standard
// OTEL_* vars at startup, so no service code change is needed. We force
// http/protobuf so the SDK targets the receiver's HTTP port (default 4318)
// instead of the gRPC default (4317), which lets Orbit run an HTTP-only
// receiver and avoid a gRPC dependency. always_on sampling is right for local
// dev where volume is low and every request matters (memory is bounded by the
// store's ingest ceilings, not by sampling).
func InjectOTEL(env map[string]string, serviceName, endpoint string) {
	if env["OTEL_EXPORTER_OTLP_ENDPOINT"] != "" {
		return // service points at its own collector — stand aside
	}
	env["OTEL_SERVICE_NAME"] = serviceName
	env["OTEL_TRACES_EXPORTER"] = "otlp"
	env["OTEL_EXPORTER_OTLP_PROTOCOL"] = "http/protobuf"
	env["OTEL_EXPORTER_OTLP_ENDPOINT"] = endpoint
	env["OTEL_TRACES_SAMPLER"] = "always_on"
}
