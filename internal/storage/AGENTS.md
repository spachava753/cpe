# Storage Package

## Purpose

`internal/storage` persists CPE message trees and ACP session state. Read
`doc.go` first: it is the canonical description of the persistence contract,
including the message metadata represented in `gai.Message.ExtraFields`.

A dialog is a root-to-leaf path through the `messages` tree. Each message has
ordered `blocks`. ACP sessions point to their latest message and may share
message history with other sessions after a fork.

## Module Boundaries

- `interfaces.go` contains the existing capability-oriented persistence contracts
  used by consumers that need substitution. Do not add storage-owned composite
  interfaces merely to hide `*Sqlite`; define an interface at the consumer only
  when multiple implementations or a useful test seam require one. ACP uses the
  concrete SQLite adapter directly.
- `MessageDB` composes the message-only interfaces. ACP session capabilities
  remain independently available to non-ACP consumers.
- `errors.go` defines the sentinel missing-record errors. Return contextual
  errors that wrap `ErrMessageNotFound` or `ErrSessionNotFound` where the
  interface promises them.
- Storage identity and lineage cross the seam through the documented
  `ExtraFields` keys. Do not expose SQLite rows or generated `sqlcgen` types
  to callers.

## SQLite Implementation

`Sqlite` is the production adapter. It enables foreign keys and initializes
its schema in `NewSqlite`; the caller owns the database handle lifecycle.
`NewConvoDB` is the process-level opener: it resolves the default centralized
path under CPE's user config directory, creates the parent directory, and
returns a `*Sqlite` that owns its database handle.

Implementation files are intentionally organized by responsibility:

- `conn.go`: default path policy, production database opening, and connection
  lifecycle.
- `sqlite.go`: database contract, adapter construction, schema embedding, and
  shared state.
- `sqlite_message_codec.go`: role conversion and the mapping between message
  `ExtraFields` and typed database columns.
- `sqlite_message_write.go`: transactional message/block writes and
  `SaveDialog`.
- `sqlite_message_read.go`: message reconstruction plus point and list reads.
- `sqlite_message_delete.go`: atomic recursive and non-recursive tree
  deletion.
- `sqlite_sessions.go`: ACP session creation, updates, reads, costs, and
  shared-history-safe deletion.

Keep a behavior change in the file that owns it. Do not reintroduce a
catch-all SQLite file or create package-level helpers merely to move a few
lines of local logic.

## Persistence Invariants

- `SaveDialog` consumes root-first chains. It validates messages that already
  have `MessageIDKey` and persists new messages in the same transaction.
- Message reads reconstruct all blocks and storage metadata, including
  parent, compaction-parent, creation, and typed agent metadata keys.
- `parent_id` is foreign-key restricted; deleting a message with descendants
  requires the explicit recursive deletion path.
- Blocks are ordered by `sequence_order` and are cascade-deleted with their
  message.
- Session deletion preserves history reachable from another ACP session.
- Session listing applies an exact `cwd` match when a filter is provided and
  preserves the unfiltered listing behavior when it is omitted.
- `NewSqlite` applies the current `schema.sql` but does not migrate older
  table layouts. Treat compatibility migrations as a separate, explicitly
  designed behavior with tests.

## SQL And Generated Code

- `schema.sql` defines the SQLite tables, indexes, and foreign keys.
- `queries.sql` is the source of named SQL operations.
- `sqlcgen/` is generated from those two files using root `sqlc.yaml`; do not
  edit it by hand.
- After intentional schema or query changes, run `sqlc generate` from the
  repository root and commit the resulting `sqlcgen` changes with their SQL
  sources.
- Keep schema, queries, codec mapping, and tests aligned whenever a stored
  field changes.

## Testing And Verification

- Keep storage tests in `sqlite_test.go`. Prefer real temporary SQLite
  databases for persistence, foreign-key, transaction, ordering, and cascade
  behavior; use the narrow `DB` seam only when simulating database failures.
- Cover both success and rollback/error paths for transaction changes.
- Preserve the documented iterator contracts, especially `SaveDialog` early
  consumer termination and read-result metadata reconstruction.
- Run `go fmt ./internal/storage`, `go vet ./internal/storage`, and
  `go test ./internal/storage` for package changes. Run `sqlc generate` before
  verification when modifying `schema.sql` or `queries.sql`.
