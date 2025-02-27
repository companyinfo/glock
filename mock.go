package glock

import (
	"context"
	"errors"
	"sync"
	"time"
)

// MockLock is a mock implementation of the Lock interface for testing.
type MockLock struct {
	acquiredLocks map[string]time.Time
	mutex         sync.Mutex
}

// NewMockLock creates a new instance of MockLock.
func NewMockLock() *MockLock {
	return &MockLock{
		acquiredLocks: make(map[string]time.Time),
	}
}

// Acquire simulates acquiring a lock.
func (m *MockLock) Acquire(_ context.Context, lockID string, ttl int64) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if expiration, exists := m.acquiredLocks[lockID]; exists {
		if time.Now().Before(expiration) {
			return ErrLockIsHeld
		}
	}

	m.acquiredLocks[lockID] = time.Now().Add(time.Duration(ttl) * time.Second)

	return nil
}

// Release simulates releasing a lock.
func (m *MockLock) Release(_ context.Context, lockID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.acquiredLocks[lockID]; !exists {
		return ErrLockIsNotHeld
	}

	delete(m.acquiredLocks, lockID)

	return nil
}

// AcquireWithRetry mocks the acquisition of a lock with retries.
func (m *MockLock) AcquireWithRetry(
	ctx context.Context,
	lockID string,
	ttl int64,
	retryInterval time.Duration,
	maxRetries int) error {
	var attempt int
	for {
		err := m.Acquire(ctx, lockID, ttl)
		if err == nil {
			return nil
		}

		if errors.Is(err, ErrLockIsHeld) && attempt < maxRetries {
			attempt++
			select {
			case <-time.After(retryInterval):
				continue
			case <-ctx.Done():
				return ErrLockTimeout
			}
		}

		return err
	}
}

// Renew simulates renewing a lock.
func (m *MockLock) Renew(_ context.Context, lockID string, ttl int64) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	_, exists := m.acquiredLocks[lockID]
	if !exists {
		return ErrLockIsNotHeld
	}

	m.acquiredLocks[lockID] = time.Now().Add(time.Duration(ttl) * time.Second)

	return nil
}
