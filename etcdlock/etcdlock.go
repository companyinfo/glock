package etcdlock

import (
	"context"
	"errors"
	"time"

	"go.etcd.io/etcd/client/v3"

	"github.com/companyinfo/glock"
)

// EtcdLock is an implementation of the distributed Lock interface using etcd.
type EtcdLock struct {
	client *clientv3.Client
}

// New creates a new EtcdLock instance.
func New(client *clientv3.Client, opts ...func(config *glock.LockConfig)) *EtcdLock {
	// Default configuration
	config := glock.DefaultConfig()

	// Apply all user-provided options
	for _, opt := range opts {
		opt(config)
	}

	return &EtcdLock{
		client: client,
	}
}

// Acquire attempts to acquire a lock on the given lockID with a TTL in seconds.
func (e *EtcdLock) Acquire(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendEtcd, glock.ActionAcquire, lockID)
	defer span.End()

	// Create a lease with the specified TTL
	lease, err := e.client.Grant(ctx, ttl)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendEtcd, glock.ActionAcquire,
			"failed to create a new lease", lockID)
	}

	// Perform a transaction to ensure the lock is acquired only if it doesn't already exist
	txn := e.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(lockID), "=", 0)). // Only succeed if the key does not exist
		Then(clientv3.OpPut(lockID, "locked", clientv3.WithLease(lease.ID))).
		Else(clientv3.OpGet(lockID)) // Retrieve the existing lock for debugging or logging

	resp, err := txn.Commit()
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendEtcd, glock.ActionAcquire,
			"failed to acquire lock", lockID)
	}

	// Check if the transaction was successful.
	if !resp.Succeeded {
		return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendEtcd, glock.ActionAcquire,
			"lock is already held by another process", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendEtcd, glock.ActionAcquiredSuccessfully, lockID)

	return nil
}

// Release removes the lock identified by lockID.
func (e *EtcdLock) Release(ctx context.Context, lockID string) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendEtcd, glock.ActionRelease, lockID)
	defer span.End()

	resp, err := e.client.Get(ctx, lockID)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendEtcd, glock.ActionRelease,
			"failed to check lock existence", lockID)
	}

	// If the lock ID does not exist, return an error
	if len(resp.Kvs) == 0 {
		return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendEtcd,
			glock.ActionRelease, "lock was not held or already released", lockID)
	}

	if _, err = e.client.Delete(ctx, lockID); err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendEtcd, glock.ActionRelease,
			"failed to release lock", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendEtcd, glock.ActionReleasedSuccessfully, lockID)

	return nil
}

// AcquireWithRetry attempts to acquire a lock on the given lockID with a TTL in seconds with retry.
func (e *EtcdLock) AcquireWithRetry(
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

		err := e.Acquire(ctx, lockID, ttl)
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
func (e *EtcdLock) Renew(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendEtcd, glock.ActionRenew, lockID)
	defer span.End()

	// Step 1: Get the lock entry from etcd.
	response, err := e.client.Get(ctx, lockID)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendEtcd, glock.ActionRenew,
			"failed to check lock existence", lockID)
	}

	// Step 2: Check if the lock exists.
	if len(response.Kvs) == 0 {
		return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendEtcd, glock.ActionRenew,
			"lock was not held or already released", lockID)
	}

	// Step 3: Extract the current value of the lock
	currentValue := string(response.Kvs[0].Value)

	// Step 4: Create a new lease with the updated TTL.
	leaseResp, err := e.client.Grant(ctx, ttl)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendEtcd, glock.ActionRenew,
			"failed to create new lease", lockID)
	}

	newLeaseID := leaseResp.ID

	// Step 5: Use a transaction to update the lock with the new lease
	txn := e.client.Txn(ctx).
		If(clientv3.Compare(clientv3.Value(lockID), "=", currentValue)).
		Then(clientv3.OpPut(lockID, currentValue, clientv3.WithLease(newLeaseID))).
		Else(clientv3.OpGet(lockID))
	resp, err := txn.Commit()
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendEtcd, glock.ActionRenew,
			"failed to renew lock", lockID)
	}

	if !resp.Succeeded {
		return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendEtcd, glock.ActionRenew,
			"lock is already held by another process", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendEtcd, glock.ActionRenewedSuccessfully, lockID)

	return nil
}
