-- schema.sql
CREATE TABLE conversations (
    id TEXT PRIMARY KEY,
    parent_id TEXT REFERENCES conversations(id),
    user_message TEXT NOT NULL,
    executor_data BLOB NOT NULL,
    created_at TIMESTAMP NOT NULL,
    model TEXT NOT NULL
);