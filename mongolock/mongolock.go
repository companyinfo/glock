package mongolock

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/companyinfo/glock"
)

// MongoLock is an implementation of the distributed Lock interface using MongoDB.
type MongoLock struct {
	client     *mongo.Client
	database   string
	collection string
	ttlField   string
}

// New creates a new MongoLock instance.
func New(client *mongo.Client, opts ...func(config *glock.LockConfig)) *MongoLock {
	// Default configuration
	config := glock.DefaultConfig()

	// Apply all user-provided options
	for _, opt := range opts {
		opt(config)
	}

	return &MongoLock{
		client:     client,
		database:   config.Database,
		collection: config.Collection,
		ttlField:   config.TTLField,
	}
}

// Acquire attempts to acquire a lock on the given lockID with a TTL in seconds.
func (m *MongoLock) Acquire(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendMongoDB, glock.ActionAcquire, lockID)
	defer span.End()

	collection := m.client.Database(m.database).Collection(m.collection)

	// Start a MongoDB session for the transaction.
	session, err := m.client.StartSession()
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendMongoDB, glock.ActionAcquire,
			"failed to start session", lockID)
	}

	defer session.EndSession(ctx)

	// Atomic transaction to insert only if the lock doesn't exist.
	err = mongo.WithSession(ctx, session, func(sc mongo.SessionContext) error {
		err = session.StartTransaction()
		if err != nil {
			return err
		}

		// Check if lock exists
		var existing bson.M
		err = collection.FindOne(sc, bson.M{"_id": lockID}).Decode(&existing)
		if err == nil {
			// Lock already exists
			_ = session.AbortTransaction(sc)

			return glock.ErrLockIsHeld
		} else if !errors.Is(mongo.ErrNoDocuments, err) {
			// Other errors
			_ = session.AbortTransaction(sc)

			return err
		}

		// Insert lock document with expiration time
		expiration := time.Now().Add(time.Duration(ttl) * time.Second)
		doc := bson.M{
			"_id":      lockID,
			m.ttlField: expiration,
		}
		_, err = collection.InsertOne(sc, doc)
		if err != nil {
			_ = session.AbortTransaction(sc)
			return err
		}

		// Commit transaction
		return session.CommitTransaction(sc)
	})

	if err != nil {
		if errors.Is(err, glock.ErrLockIsHeld) {
			return glock.HandleError(ctx, span, err, glock.BackendMongoDB, glock.ActionAcquire,
				"lock is already held by another process", lockID)
		}

		return glock.HandleError(ctx, span, err, glock.BackendMongoDB, glock.ActionAcquire,
			"failed to acquire lock", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendMongoDB, glock.ActionAcquiredSuccessfully, lockID)

	return nil
}

// Release removes the lock identified by lockID.
func (m *MongoLock) Release(ctx context.Context, lockID string) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendMongoDB, glock.ActionRelease, lockID)
	defer span.End()

	collection := m.client.Database(m.database).Collection(m.collection)

	// Start a MongoDB session for the transaction
	session, err := m.client.StartSession()
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendMongoDB, glock.ActionRelease,
			"failed to start session", lockID)
	}

	defer session.EndSession(ctx)

	// Atomic transaction to delete the lock
	err = mongo.WithSession(ctx, session, func(sc mongo.SessionContext) error {
		err = session.StartTransaction()
		if err != nil {
			return err
		}

		// Ensure lock exists before deleting
		var existing bson.M
		err = collection.FindOne(sc, bson.M{"_id": lockID}).Decode(&existing)
		if errors.Is(mongo.ErrNoDocuments, err) {
			_ = session.AbortTransaction(sc)

			return glock.ErrLockIsNotHeld // Lock is not held
		} else if err != nil {
			_ = session.AbortTransaction(sc)

			return err // Some other error occurred
		}

		// Delete the lock
		res, err := collection.DeleteOne(sc, bson.M{"_id": lockID})
		if err != nil {
			_ = session.AbortTransaction(sc)

			return err
		}
		if res.DeletedCount == 0 {
			_ = session.AbortTransaction(sc)

			return glock.ErrLockIsNotHeld
		}

		// Commit transaction
		return session.CommitTransaction(sc)
	})

	if err != nil {
		if errors.Is(err, glock.ErrLockIsNotHeld) {
			return glock.HandleError(ctx, span, err, glock.BackendMongoDB, glock.ActionAcquire,
				"lock was not held or already released", lockID)
		}

		return glock.HandleError(ctx, span, err, glock.BackendMongoDB, glock.ActionRelease,
			"failed to release lock", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendMongoDB, glock.ActionReleasedSuccessfully, lockID)

	return nil
}

// AcquireWithRetry attempts to acquire a lock on the given lockID with a TTL in seconds with retry.
func (m *MongoLock) AcquireWithRetry(
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

		err := m.Acquire(ctx, lockID, ttl)
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
func (m *MongoLock) Renew(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendMongoDB, glock.ActionRenew, lockID)
	defer span.End()

	collection := m.client.Database(m.database).Collection(m.collection)

	// Start a MongoDB session for the transaction
	session, err := m.client.StartSession()
	if err != nil {
		return glock.HandleError(ctx, span, err, glock.BackendMongoDB, glock.ActionRenew,
			"failed to start session", lockID)
	}

	defer session.EndSession(ctx)

	// Atomic transaction to update the TTL
	err = mongo.WithSession(ctx, session, func(sc mongo.SessionContext) error {
		err = session.StartTransaction()
		if err != nil {
			return err
		}

		// Ensure lock exists
		var existing bson.M
		err = collection.FindOne(sc, bson.M{"_id": lockID}).Decode(&existing)
		if errors.Is(mongo.ErrNoDocuments, err) {
			_ = session.AbortTransaction(sc)

			return glock.ErrLockIsNotHeld // Lock does not exist
		} else if err != nil {
			_ = session.AbortTransaction(sc)

			return err
		}

		// Update TTL
		newExpiration := time.Now().Add(time.Duration(ttl) * time.Second)
		res, err := collection.UpdateOne(sc, bson.M{"_id": lockID}, bson.M{
			"$set": bson.M{m.ttlField: newExpiration},
		})
		if err != nil {
			_ = session.AbortTransaction(sc)

			return err
		}

		if res.ModifiedCount == 0 {
			_ = session.AbortTransaction(sc)

			return glock.ErrLockIsNotHeld
		}

		// Commit transaction
		return session.CommitTransaction(sc)
	})

	if err != nil {
		if errors.Is(err, glock.ErrLockIsNotHeld) {
			return glock.HandleError(ctx, span, err, glock.BackendMongoDB, glock.ActionRenew,
				"lock was not held or already released", lockID)
		}

		return glock.HandleError(ctx, span, err, glock.BackendMongoDB, glock.ActionRenew,
			"failed to renew lock", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendMongoDB, glock.ActionRenewedSuccessfully, lockID)

	return nil
}
