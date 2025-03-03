package dynamolock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"go.companyinfo.dev/glock"
)

// DynamoDBLock is an implementation of the distributed Lock interface using DynamoDB.
type DynamoDBLock struct {
	client    *dynamodb.Client
	table     string
	lockField string
	ttlField  string
}

// New creates a new DynamoDBLock instance.
func New(client *dynamodb.Client, opts ...func(config *glock.LockConfig)) *DynamoDBLock {
	// Default configuration
	config := glock.DefaultConfig()

	// Apply all user-provided options
	for _, opt := range opts {
		opt(config)
	}

	return &DynamoDBLock{
		client:    client,
		table:     config.Table,
		lockField: config.LockField,
		ttlField:  config.TTLField,
	}
}

// Acquire attempts to acquire a lock on the given lockID with a TTL in seconds.
func (d *DynamoDBLock) Acquire(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendDynamoDB, glock.ActionAcquire, lockID)
	defer span.End()

	now := startTime.Unix()
	ttlTimestamp := now + ttl

	// Define the item for the lock.
	item := map[string]types.AttributeValue{
		d.lockField: &types.AttributeValueMemberS{Value: lockID},
		d.ttlField: &types.AttributeValueMemberN{
			Value: fmt.Sprintf("%d", ttlTimestamp),
		},
	}

	if _, err := d.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{
				Put: &types.Put{
					TableName: aws.String(d.table),
					Item:      item,
					ConditionExpression: aws.String(fmt.Sprintf("attribute_not_exists(%s) OR %s < :now",
						d.lockField, d.ttlField)),
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":now": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", now)},
					},
				},
			},
		},
	}); err != nil {
		var txnErr *types.TransactionCanceledException
		if errors.As(err, &txnErr) {
			for _, reason := range txnErr.CancellationReasons {
				if reason.Code != nil && *reason.Code == "ConditionalCheckFailed" {
					return glock.HandleError(ctx, span, glock.ErrLockIsHeld, glock.BackendDynamoDB,
						glock.ActionAcquire, "lock is already held by another process", lockID)
				}
			}

			return glock.HandleError(ctx, span, err, glock.BackendDynamoDB, glock.ActionAcquire,
				"transaction failed", lockID)
		}

		return glock.HandleError(ctx, span, err, glock.BackendDynamoDB, glock.ActionAcquire,
			"failed to acquire lock", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendDynamoDB, glock.ActionAcquiredSuccessfully, lockID)

	return nil
}

// Release removes the lock identified by lockID.
func (d *DynamoDBLock) Release(ctx context.Context, lockID string) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendDynamoDB, glock.ActionRelease, lockID)
	defer span.End()

	if _, err := d.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{
				Delete: &types.Delete{
					TableName: aws.String(d.table),
					Key: map[string]types.AttributeValue{
						d.lockField: &types.AttributeValueMemberS{Value: lockID},
					},
				},
			},
		},
	}); err != nil {
		var cfe *types.ConditionalCheckFailedException
		if errors.As(err, &cfe) {
			return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendDynamoDB,
				glock.ActionRelease, "lock was not held or already released", lockID)
		}

		return glock.HandleError(ctx, span, err, glock.BackendDynamoDB, glock.ActionRelease,
			"failed to release lock", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendDynamoDB, glock.ActionReleasedSuccessfully, lockID)

	return nil
}

// AcquireWithRetry attempts to acquire a lock on the given lockID with a TTL in seconds with retry.
func (d *DynamoDBLock) AcquireWithRetry(
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

		err := d.Acquire(ctx, lockID, ttl)
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
func (d *DynamoDBLock) Renew(ctx context.Context, lockID string, ttl int64) error {
	startTime := time.Now()
	ctx, span := glock.RecordStart(ctx, glock.BackendDynamoDB, glock.ActionRenew, lockID)
	defer span.End()

	ttlTimestamp := time.Now().Add(time.Duration(ttl) * time.Second).Unix()
	update := &dynamodb.UpdateItemInput{
		TableName: aws.String(d.table),
		Key: map[string]types.AttributeValue{
			d.lockField: &types.AttributeValueMemberS{Value: lockID},
		},
		UpdateExpression: aws.String(fmt.Sprintf("SET %s = :ttl", d.ttlField)),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":ttl": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", ttlTimestamp)},
		},
		ConditionExpression: aws.String(fmt.Sprintf("attribute_exists(%s)", d.lockField)),
	}

	if _, err := d.client.UpdateItem(ctx, update); err != nil {
		var cfe *types.ConditionalCheckFailedException
		if errors.As(err, &cfe) {
			return glock.HandleError(ctx, span, glock.ErrLockIsNotHeld, glock.BackendDynamoDB,
				glock.ActionRenew, "lock was not held or already released", lockID)
		}

		return glock.HandleError(ctx, span, err, glock.BackendDynamoDB, glock.ActionRenew,
			"failed to renew lock", lockID)
	}

	glock.RecordSuccess(ctx, span, startTime, glock.BackendDynamoDB, glock.ActionRenewedSuccessfully, lockID)

	return nil
}
