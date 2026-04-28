/*
Package storage defines CPE conversation persistence contracts and
implementations.

Conversations are stored as message trees (parent-child lineage) where each
message contains ordered content blocks. The package exposes narrow interfaces
(DialogSaver, MessagesGetter, MessagesLister, MessagesDeleter) plus the
composed MessageDB interface.

Implementations:
  - Sqlite: production backend backed by .cpeconvo with transactional writes,
    referential integrity, and migration helpers.
  - MemDB: in-memory backend used primarily for tests.

Message metadata contract:
returned gai.Message values include storage metadata in ExtraFields using
MessageIDKey, MessageParentIDKey, MessageCompactionParentIDKey, and
MessageCreatedAtKey to keep persistence details available without leaking
DB-specific ports. JSON-compatible message-level ExtraFields are persisted
across save/load; known agent metadata keys are also stored in typed SQLite
columns for lightweight analysis.
*/
package storage
