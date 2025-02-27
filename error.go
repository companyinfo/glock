package glock

import "errors"

var (
	// ErrLockIsHeld lock is already held by another process.
	ErrLockIsHeld = errors.New("lock is already held by another process")
	// ErrLockIsNotHeld lock was not held or already released.
	ErrLockIsNotHeld = errors.New("lock was not held or already released")
	// ErrLockTimeout lock acquisition timed out.
	ErrLockTimeout = errors.New("lock acquisition timed out")
)
