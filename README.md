# Golang Distributed Lock (glock)

[![Go Report Card](https://goreportcard.com/badge/github.com/companyinfo/glock)](https://goreportcard.com/report/github.com/companyinfo/glock)
[![License](https://img.shields.io/github/license/companyinfo/glock)](LICENSE)
[![GoDoc](https://pkg.go.dev/badge/github.com/companyinfo/glock.svg)](https://pkg.go.dev/github.com/companyinfo/glock)

A **Golang Distributed Lock** package with multiple backends, supporting **DynamoDB, Redis, etcd, Consul, ZooKeeper, Hazelcast, MongoDB, and PostgreSQL**. Built for **high availability**, **fault tolerance**, and **performance**, with **OpenTelemetry** support for tracing and metrics.

## Features 🚀
✅ Supports multiple **storage backends**  
✅ Provides **Acquire**, **Release**, **Renew**, and **AcquireWithRetry** functions  
✅ **Atomic operations** for safe concurrency control  
✅ Implements **OpenTelemetry** for tracing and metrics  
✅ Uses **functional options pattern** for extensibility  

## Installation 📦
```sh
go get go.companyinfo.dev/glock
```

## Usage 🛠️

### Initialize the Lock Manager
Each backend requires specific configuration. Below is an example.

#### **Redis**
```go
package main

import (
    "context"
    "fmt"

    "github.com/go-logr/logr"
    "github.com/go-redis/redis/v8"

    "go.companyinfo.dev/glock"
    "go.companyinfo.dev/glock/redislock"
)

func main() {
    logger := logr.Logger{}.V(1).WithName("distributed-lock")
    redisClient := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
    lock := redislock.New(redisClient, glock.WithLogger(logger))

    if err := lock.Acquire(context.Background(), "prod:lock:object:12", 10); err != nil {
        fmt.Println("Failed to acquire lock")
    }

    defer lock.Release(context.Background(), "prod:lock:object:12")
}
```

## Supported Backends 🔌
| Backend        | Implementation                     |
|----------------|------------------------------------|
| **DynamoDB**   | ✅ `dynamolock.New`                 |
| **Redis**      | ✅ `redislock.New`                  |
| **etcd**       | ✅ `etcdlock.New`           |
| **Consul**     | ✅ `consullock.New`         |
| **ZooKeeper**  | ✅ `zookeeperlock.New` |
| **Hazelcast**  | ✅ `hazelcastlock.New` |
| **MongoDB**    | ✅ `mongolock.New`         |
| **PostgreSQL** | ✅ `postgreslock.New`   |

## OpenTelemetry Integration 📊
This package supports **OpenTelemetry** for distributed tracing and metrics.

### **Metrics Supported**
- `lock_acquire_total`
- `lock_acquire_latency_seconds`
- `lock_release_total`
- `lock_release_latency_seconds`
- `lock_renew_total`
- `lock_renew_latency_seconds`

## License 📜
This project is licensed under the **MIT License**. See [LICENSE](LICENSE) for details.

---
