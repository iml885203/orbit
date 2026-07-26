package health

import "time"

// Result represents the outcome of a health check.
type Result struct {
	Service string
	Healthy bool
	Message string
	Latency time.Duration
}
