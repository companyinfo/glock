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
	// ActionAcquire represents the action of attempting to acquire a lock.
	ActionAcquire = "acquire"
	// ActionRelease represents the action of releasing a previously acquired lock.
	ActionRelease = "release"
	// ActionRenew represents the action of extending the expiration time of a lock.
	ActionRenew = "renew"
	// ActionAcquiredSuccessfully indicates that a lock was successfully acquired.
	ActionAcquiredSuccessfully = "acquired"
	// ActionReleasedSuccessfully indicates that a lock was successfully released.
	ActionReleasedSuccessfully = "released"
	// ActionRenewedSuccessfully indicates that a lock was successfully renewed.
	ActionRenewedSuccessfully = "renewed"
)

const (
	// BackendConsul represents Consul as a distributed locking backend.
	BackendConsul = "consul"
	// BackendEtcd represents etcd as a distributed locking backend.
	BackendEtcd = "etcd"
	// BackendDynamoDB represents AWS DynamoDB as a distributed locking backend.
	BackendDynamoDB = "dynamodb"
	// BackendHazelcast represents Hazelcast as a distributed locking backend.
	BackendHazelcast = "hazelcast"
	// BackendMongoDB represents MongoDB as a distributed locking backend.
	BackendMongoDB = "mongodb"
	// BackendRedis represents Redis as a distributed locking backend.
	BackendRedis = "redis"
	// BackendZooKeeper represents Apache ZooKeeper as a distributed locking backend.
	BackendZooKeeper = "zookeeper"
	// BackendPostgres represents PostgreSQL as a distributed locking backend.
	BackendPostgres = "postgres"
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
