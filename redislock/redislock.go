package redislock

import (
	"context"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"

	"go.companyinfo.dev/glock"
)

// RedisLock is an implementation of the distributed Lock interface using Redis.
type RedisLock struct {
	client *redis.Client
}

// New creates a new RedisLock instance.
func New(client *redis.Client, opts ...func(config *glock.LockConfig)) *RedisLock {
	// Default configuration
	config := glock.DefaultConfig()

	// Apply all user-provided options
	for _, opt := range opts {
		opt(config)
	}

	return &RedisLock{
		client: client,
	}
}

// Acquire attempts to acquire a lock on the given lockID with a TTL in seconds.
func (r *RedisLock) Acquire(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendRedis, glock.ActionAcquire, lockID)
	defer span.End()

	// Use Redis SETNX (set if not exists) to create the lock.
	success, err := r.client.SetNX(ctx, lockID, "locked", time.Duration(ttl)*time.Second).Result()
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendRedis, glock.ActionAcquire,
			"failed to acquire lock", lockID)
	}

	if !success {
		return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendRedis, glock.ActionAcquire,
			"lock is already held by another process", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendRedis, glock.ActionAcquiredSuccessfully, lockID)

	return nil
}

// Release removes the lock identified by lockID.
func (r *RedisLock) Release(ctx context.Context, lockID string) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendRedis, glock.ActionRelease, lockID)
	defer span.End()

	result, err := r.client.Del(ctx, lockID).Result()
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendRedis, glock.ActionRelease,
			"failed to release lock", lockID)
	}

	if result == 0 {
		return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendRedis,
			glock.ActionRelease, "lock was not held or already released", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendRedis, glock.ActionReleasedSuccessfully, lockID)

	return nil
}

// AcquireWithRetry attempts to acquire a lock on the given lockID with a TTL in seconds with retry.
func (r *RedisLock) AcquireWithRetry(
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

		err := r.Acquire(ctx, lockID, ttl)
		if err == nil {
			return nil
		}

		if errors.Is(err, glock.ErrLockIsHeld) && attempt < maxRetries {
			attempt++
			select {
			case <-time.After(retryInterval):
				continue
			case <-ctx.Done():
				glock.GetLogger().Error(glock.ErrLockTimeout, "lock acquisition timed out due to context cancellation",
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
func (r *RedisLock) Renew(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendRedis, glock.ActionRenew, lockID)
	defer span.End()

	script := `
		if redis.call("exists", KEYS[1]) == 1 then
			return redis.call("pexpire", KEYS[1], ARGV[1])
		else
			return 0
		end
	`

	result, err := r.client.Eval(ctx, script, []string{lockID}, ttl*1000).Result()
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendRedis, glock.ActionRenew,
			"failed to renew lock", lockID)
	}

	if result.(int64) == 0 {
		return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendRedis,
			glock.ActionRenew, "lock was not held or already released", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendRedis, glock.ActionRenewedSuccessfully, lockID)

	return nil
}
