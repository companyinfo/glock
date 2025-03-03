package postgreslock

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"go.companyinfo.dev/glock"
)

// PostgresLock is an implementation of the distributed Lock interface using PostgresSQL.
type PostgresLock struct {
	client    *sql.DB
	table     string
	lockField string
	ttlField  string
}

// New creates a new PostgresLock instance.
func New(client *sql.DB, opts ...func(config *glock.LockConfig)) *PostgresLock {
	// Default configuration
	config := glock.DefaultConfig()

	// Apply all user-provided options
	for _, opt := range opts {
		opt(config)
	}

	return &PostgresLock{
		client:    client,
		table:     config.Table,
		lockField: config.LockField,
		ttlField:  config.TTLField,
	}
}

// Acquire attempts to acquire a lock on the given lockID with a TTL in seconds.
func (p *PostgresLock) Acquire(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendPostgres, glock.ActionAcquire, lockID)
	defer span.End()

	expirationTime := time.Now().Add(time.Duration(ttl) * time.Second)
	tx, err := p.client.BeginTx(ctx, nil)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendPostgres, glock.ActionAcquire,
			"failed to begin transaction", lockID)
	}

	defer func() {
		if err = tx.Rollback(); err != nil {
			glock.GetLogger().Error(err, "failed to rollback transaction", "lockID", lockID)
		}
	}()

	// Insert or update the lock with expiration time atomically
	query := fmt.Sprintf(`
    INSERT INTO %s (%s, %s) 
    VALUES ($1, $2)
    ON CONFLICT (%s)
    DO UPDATE SET %s = EXCLUDED.%s
    WHERE %s.%s < NOW()`,
		p.table, p.lockField, p.ttlField, p.lockField, p.ttlField, p.ttlField, p.table, p.ttlField) // #nosec G201
	result, err := tx.ExecContext(ctx, query, lockID, expirationTime)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendPostgres, glock.ActionAcquire,
			"failed to insert/update lock", lockID)
	}

	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendPostgres, glock.ActionAcquire,
			"lock is already held by another process", lockID)
	}

	if err = tx.Commit(); err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendPostgres, glock.ActionAcquire,
			"failed to acquire lock", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendPostgres, glock.ActionAcquiredSuccessfully, lockID)

	return nil
}

// Release removes the lock identified by lockID.
func (p *PostgresLock) Release(ctx context.Context, lockID string) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendPostgres, glock.ActionRelease, lockID)
	defer span.End()

	tx, err := p.client.BeginTx(ctx, nil)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendPostgres, glock.ActionRelease,
			"failed to begin transaction", lockID)
	}

	defer func() {
		if err = tx.Rollback(); err != nil {
			glock.GetLogger().Error(err, "failed to rollback transaction", "lockID", lockID)
		}
	}()

	// Check if the lock exists and is not expired.
	var expirationTime time.Time
	query := fmt.Sprintf(`SELECT %s FROM %s WHERE %s = $1`, p.ttlField, p.table, p.lockField) // #nosec G201
	if err = tx.QueryRowContext(ctx, query, lockID).Scan(&expirationTime); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendPostgres,
				glock.ActionRelease, "lock was not held or already released", lockID)
		}

		return glock.HandleError(ctx, span, err, glock.BackendPostgres, glock.ActionRelease,
			"failed to query lock", lockID)
	}

	if time.Now().After(expirationTime) {
		return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendPostgres,
			glock.ActionRelease, "lock was not held or already released", lockID)
	}

	// Delete the lock from the locks table
	query = fmt.Sprintf(`DELETE FROM %s WHERE %s = $1`, p.table, p.lockField) // #nosec G201
	if _, err = tx.ExecContext(ctx, query, lockID); err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendPostgres, glock.ActionRelease,
			"failed to delete lock", lockID)
	}

	if err = tx.Commit(); err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendPostgres, glock.ActionRelease,
			"failed to release lock", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendPostgres, glock.ActionReleasedSuccessfully, lockID)

	return nil
}

// AcquireWithRetry attempts to acquire a lock on the given lockID with a TTL in seconds with retry.
func (p *PostgresLock) AcquireWithRetry(
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

		err := p.Acquire(ctx, lockID, ttl)
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
func (p *PostgresLock) Renew(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendPostgres, glock.ActionRenew, lockID)
	defer span.End()

	tx, err := p.client.BeginTx(ctx, nil)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendPostgres, glock.ActionRenew,
			"failed to start transaction", lockID)
	}

	defer func() {
		if err = tx.Rollback(); err != nil {
			glock.GetLogger().Error(err, "failed to rollback transaction", "lockID", lockID)
		}
	}()

	// Update the expiration_time only if the current lock hasn't expired
	query := fmt.Sprintf(`
        UPDATE %s
        SET %s = NOW() + INTERVAL '1 second' * $1
        WHERE %s = $2 AND %s > NOW()`,
		p.table, p.ttlField, p.lockField, p.ttlField) // #nosec G201
	result, err := tx.ExecContext(ctx, query, ttl, lockID)
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendPostgres, glock.ActionRenew,
			"failed to renew", lockID)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendPostgres,
			glock.ActionRenew, "lock was not held or already released", lockID)
	}

	if err = tx.Commit(); err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendPostgres, glock.ActionRenew,
			"failed to renew", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendPostgres, glock.ActionRenewedSuccessfully, lockID)

	return nil
}
