package glock

import (
	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	Name              string = "distributed_lock"
	DefaultLogLevel   int    = 0
	DefaultLoggerName string = "distributed_lock"
	DefaultTable      string = "distributed_lock"
	DefaultTTLField   string = "expiration_time"
	DefaultLockField  string = "lock_id"
	DefaultMap        string = "distributed_lock"
	DefaultDatabase   string = "distributed_lock"
	DefaultCollection string = "locks"
)

// OptionFunc A function type used to apply custom configurations to LockConfig.
type OptionFunc func(*LockConfig)

// LockConfig A struct holding configuration settings such as logger, database/table names,
// and OpenTelemetry tracer/meter.
type LockConfig struct {
	Table      string
	TTLField   string
	LockField  string
	Map        string
	Database   string
	Collection string
}

// DefaultConfig returns a LockConfig with default values, including:
// - A default logger with predefined log level and name.
// - An OpenTelemetry tracer and meter for distributed tracing and metrics.
func DefaultConfig() *LockConfig {
	InitializeLogger(logr.Logger{}.V(DefaultLogLevel).WithName(DefaultLoggerName)) // Set up global logger.
	InitializeTracing(otel.GetTracerProvider())                                    // Set up global tracing.
	InitializeMetrics(otel.GetMeterProvider())                                     // Set up global Meter.

	return &LockConfig{
		Table:      DefaultTable,
		TTLField:   DefaultTTLField,
		LockField:  DefaultLockField,
		Map:        DefaultMap,
		Database:   DefaultDatabase,
		Collection: DefaultCollection,
	}
}

// WithTable sets the table name for storage backends that use tables (e.g., DynamoDB, PostgreSQL).
func WithTable(name string) OptionFunc {
	return func(cfg *LockConfig) {
		cfg.Table = name
	}
}

// WithTTLField sets the TTL (expiration) field name in the storage backend.
// This field is used to track lock expiration.
func WithTTLField(name string) OptionFunc {
	return func(cfg *LockConfig) {
		cfg.TTLField = name
	}
}

// WithLockField sets the lock identifier field name in the storage backend.
// This field uniquely identifies a lock record.
func WithLockField(name string) OptionFunc {
	return func(cfg *LockConfig) {
		cfg.LockField = name
	}
}

// WithMapName sets the map name for storage backends that use key-value maps (e.g., Hazelcast).
func WithMapName(name string) OptionFunc {
	return func(cfg *LockConfig) {
		cfg.Map = name
	}
}

// WithDatabase sets the database name for storage backends that require a database name (e.g., MongoDB, PostgreSQL).
func WithDatabase(name string) OptionFunc {
	return func(cfg *LockConfig) {
		cfg.Database = name
	}
}

// WithCollection sets the collection name for NoSQL storage backends like MongoDB.
func WithCollection(name string) OptionFunc {
	return func(cfg *LockConfig) {
		cfg.Collection = name
	}
}

// WithLogger sets a custom logger in LockConfig.
// This allows users to integrate their own logging implementation.
func WithLogger(logger logr.Logger) OptionFunc {
	return func(cfg *LockConfig) {
		InitializeLogger(logger)
	}
}

// WithTracerProvider sets a custom OpenTelemetry tracer provider for distributed tracing.
// If not set, the default OpenTelemetry tracer is used.
func WithTracerProvider(tp trace.TracerProvider) OptionFunc {
	return func(cfg *LockConfig) {
		InitializeTracing(tp)
	}
}

// WithMeterProvider sets a custom OpenTelemetry meter provider for capturing metrics.
// If not set, the default OpenTelemetry meter is used.
func WithMeterProvider(mp metric.MeterProvider) OptionFunc {
	return func(cfg *LockConfig) {
		InitializeMetrics(mp)
	}
}
