CREATE TABLE sessions (
	id TEXT PRIMARY KEY NOT NULL,
	parent_id TEXT,
	cwd TEXT NOT NULL,
	title TEXT NOT NULL,
	-- Microseconds since the Unix epoch, UTC.
	updated_at INTEGER NOT NULL,
	options TEXT NOT NULL,
	usage TEXT NOT NULL,
	-- NULL until the session is compacted.
	compaction TEXT
) STRICT;

CREATE INDEX sessions_by_recency ON sessions (updated_at DESC, id DESC);

CREATE INDEX sessions_by_cwd ON sessions (cwd, updated_at DESC, id DESC);

CREATE TABLE messages (
	session_id TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
	-- Zero-based index of the message in the transcript.
	position INTEGER NOT NULL,
	-- SHA-256 of data, compared to find the first changed message on save.
	digest BLOB NOT NULL,
	data TEXT NOT NULL,
	PRIMARY KEY (session_id, position)
) STRICT;
