package hazelcastlock

import (
	"context"
	"errors"
	"time"

	"github.com/hazelcast/hazelcast-go-client"

	"github.com/companyinfo/glock"
)

// HazelcastLock is an implementation of the distributed Lock interface using Hazelcast.
type HazelcastLock struct {
	client  *hazelcast.Client
	mapName string
}

// New creates a new HazelcastLock instance.
func New(client *hazelcast.Client, opts ...func(config *glock.LockConfig)) *HazelcastLock {
	// Default configuration
	config := glock.DefaultConfig()

	// Apply all user-provided options
	for _, opt := range opts {
		opt(config)
	}

	return &HazelcastLock{
		client:  client,
		mapName: config.Map,
	}
}

// Acquire attempts to acquire a lock on the given lockID with a TTL in seconds.
func (h *HazelcastLock) Acquire(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendHazelcast, glock.ActionAcquire, lockID)
	defer span.End()

	lockMap, err := h.client.GetMap(ctx, h.mapName)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendHazelcast, glock.ActionAcquire,
			"failed to get lock map", lockID)
	}

	// Check if lock is already held before acquiring
	locked, err := lockMap.IsLocked(ctx, lockID)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendHazelcast, glock.ActionAcquire,
			"failed to check lock state", lockID)
	}

	if locked {
		return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendHazelcast,
			glock.ActionAcquire, "lock is already held by another process", lockID)
	}

	acquired, err := lockMap.TryLockWithLease(ctx, lockID, time.Duration(ttl)*time.Second)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendHazelcast, glock.ActionAcquire,
			"failed to acquire lock", lockID)
	}

	if !acquired {
		return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendHazelcast,
			glock.ActionAcquire, "lock is already held by another process", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendHazelcast, glock.ActionAcquiredSuccessfully, lockID)

	return nil
}

// Release removes the lock identified by lockID.
func (h *HazelcastLock) Release(ctx context.Context, lockID string) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendHazelcast, glock.ActionRelease, lockID)
	defer span.End()

	lockMap, err := h.client.GetMap(ctx, h.mapName)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendHazelcast, glock.ActionRelease,
			"failed to get lock map", lockID)
	}

	// Verify that the lock ID is held by the current process (or expected holder)
	locked, err := lockMap.IsLocked(ctx, lockID)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendHazelcast, glock.ActionRelease,
			"failed to check lock status", lockID)
	}

	if !locked {
		return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendHazelcast,
			glock.ActionRelease, "lock was not held or already released", lockID)
	}

	if err = lockMap.Unlock(ctx, lockID); err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendHazelcast, glock.ActionRelease,
			"failed to release lock", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendHazelcast, glock.ActionReleasedSuccessfully, lockID)

	return nil
}

// AcquireWithRetry attempts to acquire a lock on the given lockID with a TTL in seconds with retry.
func (h *HazelcastLock) AcquireWithRetry(
	ctx context.Context,
	lockID string,
	ttl int64,
	retryInterval time.Duration,
	maxRetries int) error {
	var attempt int
	for {
		glock.GetLogger().Info("attempting to acquire lock",
			"lockID", lockID,
			"attempt", attempt+1,
			"maxRetries", maxRetries,
			"retryInterval", retryInterval)

		err := h.Acquire(ctx, lockID, ttl)
		if err == nil {
			return nil
		}

		if errors.Is(err, glock.ErrLockIsHeld) && attempt < maxRetries {
			attempt++
			select {
			case <-time.After(retryInterval):
				continue
			case <-ctx.Done():
				glock.GetLogger().Error(glock.ErrLockTimeout,
					"lock acquisition timed out due to context cancellation",
					"lockID", lockID)

				return glock.ErrLockTimeout
			}
		}

		glock.GetLogger().Error(err, "failed to acquire lock after multiple attempts",
			"lockID", lockID,
			"attempt", attempt,
			"maxRetries", maxRetries)

		return err
	}
}

// Renew extends the TTL of an existing lock.
func (h *HazelcastLock) Renew(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendHazelcast, glock.ActionRenew, lockID)
	defer span.End()

	lockMap, err := h.client.GetMap(ctx, h.mapName)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendHazelcast, glock.ActionRenew,
			"failed to get lock map", lockID)
	}

	// Check if the lock is held
	locked, err := lockMap.IsLocked(ctx, lockID)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendHazelcast, glock.ActionRenew,
			"failed to check lock state", lockID)
	}

	if !locked {
		return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendHazelcast,
			glock.ActionRenew, "lock was not held or already released", lockID)
	}

	// Atomic operation: extend the lease without releasing the lock.
	if err = lockMap.ForceUnlock(ctx, lockID); err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendHazelcast, glock.ActionRenew,
			"failed to unlock for renewal", lockID)
	}

	acquired, err := lockMap.TryLockWithLease(ctx, lockID, time.Duration(ttl)*time.Second)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendHazelcast, glock.ActionRenew,
			"failed to reacquire lock for renewal", lockID)
	}

	if !acquired {
		return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendHazelcast,
			glock.ActionRenew, "lock is already held by another process", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendHazelcast, glock.ActionRenewedSuccessfully, lockID)

	return nil
}
