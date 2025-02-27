package zookeeperlock

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-zookeeper/zk"

	"github.com/companyinfo/glock"
)

// ZooKeeperLock is an implementation of the distributed Lock interface using ZooKeeper.
type ZooKeeperLock struct {
	client *zk.Conn
}

// New creates a new ZooKeeperLock instance.
func New(client *zk.Conn, opts ...func(config *glock.LockConfig)) *ZooKeeperLock {
	// Default configuration
	config := glock.DefaultConfig()

	// Apply all user-provided options
	for _, opt := range opts {
		opt(config)
	}

	return &ZooKeeperLock{
		client: client,
	}
}

// Acquire attempts to acquire a lock on the given lockID with a TTL in seconds.
func (z *ZooKeeperLock) Acquire(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendZooKeeper, glock.ActionAcquire, lockID)
	defer span.End()

	// Ensure lockID is a valid ZooKeeper path
	if !strings.HasPrefix(lockID, "/") {
		lockID = "/" + lockID // Ensure it starts with "/"
	}

	if err := validateZooKeeperPath(lockID); err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendZooKeeper, glock.ActionAcquire,
			"invalid lock path", lockID)
	}

	expiration := time.Now().Add(time.Duration(ttl) * time.Second).Unix()
	lockData := []byte(fmt.Sprintf("%d", expiration))

	// Attempt to create the lock node (atomic if node doesn't exist)
	_, err := z.client.Create(lockID, lockData, zk.FlagEphemeral, zk.WorldACL(zk.PermAll))
	if err == nil {
		glock.RecordSuccess(ctx, span, startTime, glock.BackendZooKeeper, glock.ActionAcquiredSuccessfully,
			lockID)

		return nil
	}

	// If the node exists, check if the lock has expired
	if errors.Is(err, zk.ErrNodeExists) {
		data, stat, err := z.client.Get(lockID)
		if err != nil {
			return glock.HandleError(ctx, span, err, glock.BackendZooKeeper, glock.ActionAcquire,
				"failed to get existing lock", lockID)
		}

		// Check expiration time
		existingExpiration, err := strconv.ParseInt(string(data), 10, 64)
		if err != nil {
			return glock.HandleError(ctx, span, err, glock.BackendZooKeeper, glock.ActionAcquire,
				"invalid lock data", lockID)
		}

		if time.Now().Unix() > existingExpiration {
			// Lock has expired, attempt to acquire by updating the node.
			_, err := z.client.Set(lockID, lockData, stat.Version)
			if err == nil {
				glock.RecordSuccess(ctx, span, startTime, glock.BackendZooKeeper,
					glock.ActionAcquiredSuccessfully, lockID)

				return nil
			}

			if errors.Is(err, zk.ErrBadVersion) {
				return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendZooKeeper,
					glock.ActionAcquire, "lock is already held by another process", lockID)
			}

			return glock.HandleError(ctx, span, err, glock.BackendZooKeeper, glock.ActionAcquire,
				"failed to acquire expired lock", lockID)
		}

		return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendZooKeeper, glock.ActionAcquire,
			"lock is already held by another process", lockID)
	}

	return glock.HandleError(ctx, span, err, glock.BackendZooKeeper, glock.ActionAcquire,
		"failed to acquire lock", lockID)
}

// Release removes the lock identified by lockID.
func (z *ZooKeeperLock) Release(ctx context.Context, lockID string) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendZooKeeper, glock.ActionRelease, lockID)
	defer span.End()

	// Ensure lockID is a valid ZooKeeper path
	if !strings.HasPrefix(lockID, "/") {
		lockID = "/" + lockID
	}

	_, stat, err := z.client.Get(lockID)
	if err != nil {
		if errors.Is(err, zk.ErrNoNode) {
			return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendZooKeeper, glock.ActionRelease,
				"lock was not held or already released", lockID)
		}

		return glock.HandleError(ctx, span, err, glock.BackendZooKeeper, glock.ActionRelease,
			"failed to get lock for release", lockID)
	}

	// Delete the lock node atomically using the version.
	if err = z.client.Delete(lockID, stat.Version); err != nil {
		if errors.Is(err, zk.ErrBadVersion) {
			return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendZooKeeper, glock.ActionRelease,
				"lock is already held by another process", lockID)
		}

		return glock.HandleError(ctx, span, err, glock.BackendZooKeeper, glock.ActionRelease,
			"failed to release lock", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendZooKeeper, glock.ActionReleasedSuccessfully, lockID)

	return nil
}

// AcquireWithRetry attempts to acquire a lock on the given lockID with a TTL in seconds with retry.
func (z *ZooKeeperLock) AcquireWithRetry(
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

		err := z.Acquire(ctx, lockID, ttl)
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
func (z *ZooKeeperLock) Renew(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendZooKeeper, glock.ActionRenew, lockID)
	defer span.End()

	// Ensure lockID is a valid ZooKeeper path
	if !strings.HasPrefix(lockID, "/") {
		lockID = "/" + lockID
	}

	expiration := time.Now().Add(time.Duration(ttl) * time.Second).Unix()
	lockData := []byte(fmt.Sprintf("%d", expiration))

	// Attempt to get the lock node and its version.
	data, stat, err := z.client.Get(lockID)
	if err != nil {
		if errors.Is(err, zk.ErrNoNode) {
			return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendZooKeeper,
				glock.ActionRenew, "lock was not held or already released", lockID)
		}

		return glock.HandleError(ctx, span, err, glock.BackendZooKeeper, glock.ActionRenew,
			"failed to get lock", lockID)
	}

	// Check if the lock is still valid
	currentExpiration, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendZooKeeper, glock.ActionRenew,
			"invalid lock data", lockID)
	}

	if time.Now().Unix() > currentExpiration {
		return glock.HandleError(ctx, span, nil, glock.BackendZooKeeper, glock.ActionRenew,
			"lock already expired", lockID)
	}

	// Attempt to renew the lock atomically using version check
	if _, err = z.client.Set(lockID, lockData, stat.Version); err != nil {
		if errors.Is(err, zk.ErrBadVersion) {
			return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendZooKeeper, glock.ActionRenew,
				"lock is already held by another process", lockID)
		}

		return glock.HandleError(ctx, span, err, glock.BackendZooKeeper, glock.ActionRenew,
			"failed to renew lock", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendZooKeeper, glock.ActionRenewedSuccessfully, lockID)

	return nil
}

// validateZooKeeperPath checks if the path is valid for ZooKeeper.
func validateZooKeeperPath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return errors.New("ZooKeeper path must start with '/'")
	}

	if strings.ContainsAny(path, " \t\n\r\000") { // No spaces or null characters allowed
		return errors.New("ZooKeeper path contains invalid characters")
	}

	return nil
}
