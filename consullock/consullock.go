package consullock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/companyinfo/glock"
)

// ConsulLock is an implementation of the distributed Lock interface using Consul.
type ConsulLock struct {
	client    *api.Client
	sessionID string
}

// New creates a new ConsulLock instance.
func New(client *api.Client, opts ...func(config *glock.LockConfig)) *ConsulLock {
	// Default configuration
	config := glock.DefaultConfig()

	// Apply all user-provided options
	for _, opt := range opts {
		opt(config)
	}

	return &ConsulLock{
		client: client,
	}
}

// Acquire attempts to acquire a lock on the given lockID with a TTL in seconds.
func (c *ConsulLock) Acquire(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendConsul, glock.ActionAcquire, lockID)
	defer span.End()

	sessionEntry := &api.SessionEntry{
		TTL:       fmt.Sprintf("%ds", ttl),
		LockDelay: 0,
		Behavior:  api.SessionBehaviorDelete, // Release locks when the session is invalidated.
	}

	sessionID, _, err := c.client.Session().Create(sessionEntry, nil)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendConsul, glock.ActionAcquire,
			"failed to create consul session", lockID)
	}

	kvPair := &api.KVPair{
		Key:     lockID,
		Session: sessionID,
	}
	acquired, _, err := c.client.KV().Acquire(kvPair, nil)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendConsul, glock.ActionAcquire,
			"failed to acquire lock", lockID)
	}

	if !acquired {
		// Clean up the session if acquisition fails.
		if _, err = c.client.Session().Destroy(sessionID, nil); err != nil {
			glock.GetLogger().Error(err, "failed to destroy session", "lockID", lockID)
		}

		return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendConsul, glock.ActionAcquire,
			"lock is already held by another process", lockID)
	}

	c.sessionID = sessionID
	glock.RecordSuccess(ctx, span, startTime, glock.BackendConsul, glock.ActionAcquiredSuccessfully, lockID)

	return nil
}

// Release removes the lock identified by lockID.
func (c *ConsulLock) Release(ctx context.Context, lockID string) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendConsul, glock.ActionRelease, lockID)
	defer span.End()

	kvPair, _, err := c.client.KV().Get(lockID, nil)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendConsul, glock.ActionRelease,
			"failed to get lock", lockID)
	}

	if kvPair == nil {
		return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendConsul, glock.ActionRelease,
			"lock was not held or already released", lockID)
	}

	if _, err = c.client.KV().Delete(lockID, nil); err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendConsul, glock.ActionRelease,
			"failed to delete lock key", lockID)
	}

	if _, err = c.client.Session().Destroy(c.sessionID, nil); err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendConsul, glock.ActionRelease,
			"failed to destroy consul session", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendConsul, glock.ActionReleasedSuccessfully, lockID)

	return nil
}

// AcquireWithRetry attempts to acquire a lock on the given lockID with a TTL in seconds with retry.
func (c *ConsulLock) AcquireWithRetry(
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
			"retryInterval", retryInterval,
		)

		err := c.Acquire(ctx, lockID, ttl)
		if err == nil {
			return nil
		}

		if errors.Is(err, glock.ErrLockIsHeld) && attempt < maxRetries {
			attempt++
			select {
			case <-time.After(retryInterval):
				continue
			case <-ctx.Done():
				glock.GetLogger().Error(
					glock.ErrLockTimeout,
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
func (c *ConsulLock) Renew(ctx context.Context, lockID string, _ int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendConsul, glock.ActionRenew, lockID)
	defer span.End()

	kvPair, _, err := c.client.KV().Get(lockID, nil)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendConsul, glock.ActionRenew,
			"failed to get consul lock", lockID)
	}

	if kvPair == nil {
		return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendConsul, glock.ActionRenew,
			"lock was not held or already released", lockID)
	}

	if _, _, err = c.client.Session().Renew(c.sessionID, nil); err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendConsul, glock.ActionRenew,
			"failed to renew consul session", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendConsul, glock.ActionRenewedSuccessfully, lockID)

	return nil
}
