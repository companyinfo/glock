package main

func main() {
	test := newTestDistributedLock("distributed:lock:entity:1").
		withPostgres().
		withDynamoDB().
		withHazelcast().
		withRedis().
		withEtcd().
		withConsul().
		withMongoDB().
		withZooKeeper()

	test.doLock()
	test.shutdown()
}
