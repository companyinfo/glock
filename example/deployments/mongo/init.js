// Initialize the replica set
rs.initiate({
    _id: "rs0",
    members: [
        {
            _id: 0,
            host: "mongodb:27017" // Use the container name and port
        }
    ]
});

// Wait for the replica set to be initialized
rs.status();

// Create the database and collection
db = db.getSiblingDB('distributed_lock');
db.createCollection('locks');

// Create a TTL index on the "expiration_time" field
db.distributed_lock.createIndex(
    { expiration_time: 1 },
    { expireAfterSeconds: 0 } // Documents will expire based on the value of expiration_time
);
