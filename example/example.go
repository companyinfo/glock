package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/go-redis/redis/v8"
	"github.com/go-zookeeper/zk"
	"github.com/hashicorp/consul/api"
	"github.com/hazelcast/hazelcast-go-client"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"go.companyinfo.dev/glock"
	"go.companyinfo.dev/glock/consullock"
	"go.companyinfo.dev/glock/dynamolock"
	"go.companyinfo.dev/glock/etcdlock"
	"go.companyinfo.dev/glock/hazelcastlock"
	"go.companyinfo.dev/glock/mongolock"
	"go.companyinfo.dev/glock/postgreslock"
	"go.companyinfo.dev/glock/redislock"
	"go.companyinfo.dev/glock/zookeeperlock"
)

type testDistributedLock struct {
	lockID          string
	locks           map[string]glock.Lock
	postgresClient  *sql.DB
	hazelcastClient *hazelcast.Client
	redisClient     *redis.Client
	etcdClient      *clientv3.Client
	zooKeeperClient *zk.Conn
	mongoClient     *mongo.Client
}

func newTestDistributedLock(lockID string) *testDistributedLock {
	return &testDistributedLock{
		lockID: lockID,
		locks:  make(map[string]glock.Lock),
	}
}

func (t *testDistributedLock) withPostgres() *testDistributedLock {
	db, err := sql.Open(
		"postgres",
		"postgres://user:password@localhost:5432/locks?sslmode=disable")
	if err != nil {
		panic(err)
	}

	t.locks[glock.BackendPostgres] = postgreslock.New(db)
	t.postgresClient = db

	return t
}

func (t *testDistributedLock) withDynamoDB() *testDistributedLock {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("localhost"),
		config.WithBaseEndpoint("http://localhost:8000"),
	)
	if err != nil {
		panic(err)
	}

	client := dynamodb.NewFromConfig(cfg)

	t.locks[glock.BackendDynamoDB] = dynamolock.New(client)

	return t
}

func (t *testDistributedLock) withHazelcast() *testDistributedLock {
	client, err := hazelcast.StartNewClient(context.TODO())
	if err != nil {
		panic(err)
	}

	t.locks[glock.BackendPostgres] = hazelcastlock.New(client)
	t.hazelcastClient = client

	return t
}

func (t *testDistributedLock) withRedis() *testDistributedLock {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	t.locks[glock.BackendRedis] = redislock.New(client)
	t.redisClient = client

	return t
}

func (t *testDistributedLock) withEtcd() *testDistributedLock {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"http://localhost:2379"},
		DialTimeout: 10 * time.Second,
	})
	if err != nil {
		panic(err)
	}

	t.locks[glock.BackendEtcd] = etcdlock.New(client)
	t.etcdClient = client

	return t
}

func (t *testDistributedLock) withZooKeeper() *testDistributedLock {
	conn, _, err := zk.Connect([]string{"localhost:2181"}, time.Second*10)
	if err != nil {
		panic(err)
	}

	t.locks[glock.BackendZooKeeper] = zookeeperlock.New(conn)
	t.zooKeeperClient = conn

	return t
}

func (t *testDistributedLock) withMongoDB() *testDistributedLock {
	client, err := mongo.Connect(context.TODO(),
		options.Client().ApplyURI("mongodb://localhost:27017/?replicaSet=rs0"))
	if err != nil {
		panic(err)
	}

	t.locks[glock.BackendMongoDB] = mongolock.New(client)
	t.mongoClient = client

	return t
}

func (t *testDistributedLock) withConsul() *testDistributedLock {
	cfg := api.DefaultConfig()
	cfg.Address = "http://localhost:8500"
	client, err := api.NewClient(cfg)
	if err != nil {
		panic(err)
	}

	t.locks[glock.BackendConsul] = consullock.New(client)

	return t
}

func (t *testDistributedLock) doLock() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for name, lock := range t.locks {
		fmt.Println("lock: ", name)
		fmt.Println("Trying to acquire lock...")
		if err := lock.Acquire(ctx, t.lockID, 100); err != nil {
			fmt.Println("Failed to acquire lock:", err)

			continue
		}

		fmt.Println("Lock acquired!")
		fmt.Println("Trying to acquire lock  again...")
		if err := lock.Acquire(ctx, t.lockID, 10); err != nil {
			fmt.Println("Lock is already held:", err)
		}

		fmt.Println("Trying to renew lock...")
		if err := lock.Renew(ctx, t.lockID, 10); err != nil {
			fmt.Println("Failed to renew lock:", err)

			continue
		}

		fmt.Println("Lock renewed!")
		time.Sleep(2 * time.Second)
		fmt.Println("Releasing lock...")
		if err := lock.Release(ctx, t.lockID); err != nil {
			fmt.Println("Failed to release lock:", err)

			continue
		}

		fmt.Println("Lock released!")
	}
}

func (t *testDistributedLock) shutdown() {
	if t.postgresClient != nil {
		if err := t.postgresClient.Close(); err != nil {
			fmt.Println("failed to close postgres connection: ", err)
		}
	}

	if t.hazelcastClient != nil {
		if err := t.hazelcastClient.Shutdown(context.TODO()); err != nil {
			fmt.Println("failed to close hazelcast connection: ", err)
		}
	}

	if t.redisClient != nil {
		if err := t.redisClient.Close(); err != nil {
			fmt.Println("failed to close redis connection: ", err)
		}
	}

	if t.etcdClient != nil {
		if err := t.etcdClient.Close(); err != nil {
			fmt.Println("failed to close etcd connection: ", err)
		}
	}

	if t.zooKeeperClient != nil {
		t.zooKeeperClient.Close()
	}

	if t.mongoClient != nil {
		if err := t.mongoClient.Disconnect(context.TODO()); err != nil {
			fmt.Println("failed to close mongo connection: ", err)
		}
	}
}
