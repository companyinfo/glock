CREATE TABLE distributed_lock (
   lock_id VARCHAR(100) PRIMARY KEY,
   expiration_time timestamp without time zone NOT NULL
);