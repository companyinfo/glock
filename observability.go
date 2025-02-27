package glock

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// RecordStart starts a new tracing span for a given operation.
func RecordStart(ctx context.Context, backend, action, lockID string) (context.Context, trace.Span) {
	GetLogger().Info(fmt.Sprintf("attempting to %s lock", action), "lockID", lockID)
	return GetTracer().Start(
		ctx,
		fmt.Sprintf("%s_lock.%s", backend, action),
		trace.WithAttributes(
			attribute.String("lock.id", lockID),
			attribute.String("backend", backend),
		),
	)
}

// HandleError logs, records metrics, and returns a formatted error.
func HandleError(
	ctx context.Context,
	span trace.Span,
	err error,
	backend, action, msg, lockID string) error {
	span.RecordError(err)
	span.SetStatus(codes.Error, msg)
	GetLogger().Error(err, msg, "lockID", lockID)
	metrics := GetMetrics()
	switch action {
	case ActionAcquire:
		metrics.lockAcquiredCounter.Add(ctx, 1,
			metric.WithAttributes(attribute.Bool("success", false), attribute.String("backend", backend)))
	case ActionRelease:
		metrics.lockReleaseCounter.Add(ctx, 1,
			metric.WithAttributes(attribute.Bool("success", false), attribute.String("backend", backend)))
	case ActionRenew:
		metrics.lockRenewCounter.Add(ctx, 1,
			metric.WithAttributes(attribute.Bool("success", false), attribute.String("backend", backend)))
	}

	return fmt.Errorf("%s: %w", msg, err)
}

// RecordSuccess logs and records success metrics.
func RecordSuccess(
	ctx context.Context,
	span trace.Span,
	startTime time.Time,
	action, backend, lockID string) {
	metrics := GetMetrics()
	GetLogger().Info(fmt.Sprintf("lock %s successfully", action), "lockID", lockID)
	duration := time.Since(startTime).Seconds()
	span.SetStatus(codes.Ok, fmt.Sprintf("lock %s", action))
	switch action {
	case ActionAcquiredSuccessfully:
		metrics.lockAcquiredCounter.Add(ctx, 1,
			metric.WithAttributes(attribute.Bool("success", true), attribute.String("backend", backend)))
		metrics.lockAcquireLatency.Record(ctx, duration, metric.WithAttributes(attribute.String("backend", backend)))
	case ActionReleasedSuccessfully:
		metrics.lockReleaseCounter.Add(ctx, 1,
			metric.WithAttributes(attribute.Bool("success", true), attribute.String("backend", backend)))
		metrics.lockReleaseLatency.Record(ctx, duration, metric.WithAttributes(attribute.String("backend", backend)))
	case ActionRenewedSuccessfully:
		metrics.lockRenewCounter.Add(ctx, 1,
			metric.WithAttributes(attribute.Bool("success", true), attribute.String("backend", backend)))
		metrics.lockRenewLatency.Record(ctx, duration, metric.WithAttributes(attribute.String("backend", backend)))
	}
}
