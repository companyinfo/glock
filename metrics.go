package glock

import "go.opentelemetry.io/otel/metric"

// LockMetrics holds common lock-related metrics.
type LockMetrics struct {
	meter               metric.Meter
	lockAcquiredCounter metric.Int64Counter
	lockAcquireLatency  metric.Float64Histogram
	lockReleaseCounter  metric.Int64Counter
	lockReleaseLatency  metric.Float64Histogram
	lockRenewCounter    metric.Int64Counter
	lockRenewLatency    metric.Float64Histogram
}

var globalMetrics *LockMetrics

// InitializeMetrics ensures metrics are only initialized once.
func InitializeMetrics(mp metric.MeterProvider) {
	m := mp.Meter(Name)

	// lockAcquiredCounter tracks the total number of lock acquisition attempts.
	lockAcquiredCounter, _ := m.Int64Counter(
		"lock_acquire_total",
		metric.WithDescription("Total number of lock acquire attempts"),
	)

	// lockAcquireLatency measures the latency (in seconds) of lock acquisition operations.
	lockAcquireLatency, _ := m.Float64Histogram(
		"lock_acquire_latency_seconds",
		metric.WithDescription("Latency of lock acquire operations"),
	)

	// lockReleaseCounter tracks the total number of lock release attempts.
	lockReleaseCounter, _ := m.Int64Counter(
		"lock_release_total",
		metric.WithDescription("Total number of lock release attempts"),
	)

	// lockReleaseLatency measures the latency (in seconds) of lock release operations.
	lockReleaseLatency, _ := m.Float64Histogram(
		"lock_release_latency_seconds",
		metric.WithDescription("Latency of lock release operations"),
	)

	// lockRenewCounter tracks the total number of lock renewal attempts.
	lockRenewCounter, _ := m.Int64Counter(
		"lock_renew_total",
		metric.WithDescription("Total number of lock renewal attempts"),
	)

	// lockRenewLatency measures the latency (in seconds) of lock renewal operations.
	lockRenewLatency, _ := m.Float64Histogram(
		"lock_renew_latency_seconds",
		metric.WithDescription("Latency of lock renewal operations"),
	)

	globalMetrics = &LockMetrics{
		meter:               m,
		lockAcquiredCounter: lockAcquiredCounter,
		lockAcquireLatency:  lockAcquireLatency,
		lockReleaseCounter:  lockReleaseCounter,
		lockReleaseLatency:  lockReleaseLatency,
		lockRenewCounter:    lockRenewCounter,
		lockRenewLatency:    lockRenewLatency,
	}
}

// GetMetrics returns the global LockMetrics instance.
func GetMetrics() *LockMetrics {
	return globalMetrics
}
