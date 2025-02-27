package glock

import "go.opentelemetry.io/otel/trace"

// LockTracer holds the global tracer.
type LockTracer struct {
	tracer trace.Tracer
}

var globalTracer *LockTracer

// InitializeTracing sets up tracing with a user-defined or default tracer provider.
func InitializeTracing(tp trace.TracerProvider) {
	globalTracer = &LockTracer{
		tracer: tp.Tracer(Name), // Use user-provided tracer provider
	}
}

// GetTracer returns the global Tracer instance.
func GetTracer() trace.Tracer {
	return globalTracer.tracer
}
