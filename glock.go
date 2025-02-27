// Package glock provides a flexible and configurable distributed locking mechanism
// that supports multiple backends such as DynamoDB, MongoDB, PostgreSQL, etc.
// It allows users to acquire, renew, and release locks in a distributed system
// while ensuring consistency and preventing race conditions.
package glock

import (
	"context"
	"time"
)

const (
	// List of distributed locking actions.
	ActionAcquire              = "acquire"
	ActionRelease              = "release"
	ActionRenew                = "renew"
	ActionAcquiredSuccessfully = "acquired"
	ActionReleasedSuccessfully = "released"
	ActionRenewedSuccessfully  = "renewed"

	// List of backend tools.
	BackendConsul    = "consul"
	BackendEtcd      = "etcd"
	BackendDynamoDB  = "dynamodb"
	BackendHazelcast = "hazelcast"
	BackendMongoDB   = "mongodb"
	BackendRedis     = "redis"
	BackendZooKeeper = "zookeeper"
	BackendPostgres  = "postgres"
)

// Lock defines the interface for distributed locking.
// Implementations must provide Acquire, AcquireWithRetry, and Release methods.
type Lock interface {
	// Acquire attempts to lock a resource identified by lockID for the specified TTL (in seconds).
	// Returns an error if the lock is already held or on failure.
	Acquire(ctx context.Context, lockID string, ttl int64) error

	// AcquireWithRetry attempts to lock a resource identified by lockID for the specified TTL (in seconds).
	// Returns an error if the lock is already held or on failure after a retry attempt.
	AcquireWithRetry(ctx context.Context, lockID string, ttl int64, retryInterval time.Duration, maxRetries int) error

	// Release unlocks a resource identified by lockID.
	// Returns an error if releasing the lock fails.
	Release(ctx context.Context, lockID string) error

	// Renew extends the TTL of an existing lock.
	// Returns an error if renewing the lock fails.
	Renew(ctx context.Context, lockID string, ttl int64) error
}
