/*
Package storage defines CPE message-tree and ACP session persistence contracts and
implementations.

ACP sessions point at message trees (parent-child lineage) where each message
contains ordered content blocks. The package exposes narrow interfaces
(DialogSaver, MessagesGetter, MessagesLister, MessagesDeleter,
ACPSessionCreator, ACPSessionMessageAdder, ACPSessionGetter,
ACPSessionsLister, ACPSessionCostAdder) plus composed interfaces such as
MessageDB.

Implementations:
  - Sqlite: production backend backed by .cpeconvo with transactional writes,
    referential integrity, and schema initialization.

Message metadata contract:
returned gai.Message values include storage metadata in ExtraFields using
MessageIDKey, MessageParentIDKey, MessageCompactionParentIDKey, and
MessageCreatedAtKey to keep persistence details available without leaking
DB-specific ports. JSON-compatible message-level ExtraFields are persisted
across save/load; known agent metadata keys are also stored in typed SQLite
columns for lightweight analysis.
*/
package storage
