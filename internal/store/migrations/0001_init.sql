-- 0001_init: core schema for MyShare.
--
-- File bytes live on disk as content-addressed blobs; this database holds only
-- metadata. IDs exposed over the API are opaque ULIDs, never filesystem paths.

CREATE TABLE blobs (
    hash       TEXT PRIMARY KEY,          -- lowercase hex sha-256 of the content
    size       INTEGER NOT NULL,
    refcount   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL           -- unix seconds
);

CREATE TABLE files (
    id           TEXT PRIMARY KEY,        -- ULID
    name         TEXT NOT NULL,           -- sanitised display name
    kind         TEXT NOT NULL DEFAULT 'file',  -- file | screenshot
    mime         TEXT NOT NULL DEFAULT 'application/octet-stream',
    size         INTEGER NOT NULL,
    hash         TEXT NOT NULL,           -- blob hash; integrity is via blobs.refcount,
                                          -- not an FK, so orphan blobs can be GC'd
                                          -- while soft-deleted rows keep their hash
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    deleted_at   INTEGER                  -- soft delete; NULL = live
);
CREATE INDEX idx_files_created  ON files(created_at)  WHERE deleted_at IS NULL;
CREATE INDEX idx_files_kind     ON files(kind)        WHERE deleted_at IS NULL;
CREATE INDEX idx_files_hash     ON files(hash);

CREATE TABLE clipboard (
    id         TEXT PRIMARY KEY,
    content    TEXT NOT NULL,
    format     TEXT NOT NULL DEFAULT 'text',  -- text | code | url | markdown
    pinned     INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX idx_clipboard_created ON clipboard(pinned DESC, created_at DESC);

CREATE TABLE snippets (
    id         TEXT PRIMARY KEY,
    title      TEXT NOT NULL DEFAULT '',
    content    TEXT NOT NULL,
    language   TEXT NOT NULL DEFAULT 'plaintext',
    pinned     INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX idx_snippets_created ON snippets(pinned DESC, updated_at DESC);

CREATE TABLE notes (
    id         TEXT PRIMARY KEY,
    title      TEXT NOT NULL DEFAULT '',
    content    TEXT NOT NULL DEFAULT '',
    pinned     INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX idx_notes_created ON notes(pinned DESC, updated_at DESC);

-- Resumable-upload sessions tracked for the Transfers tab and for cleanup of
-- abandoned uploads. One row per tus upload id.
CREATE TABLE upload_sessions (
    id          TEXT PRIMARY KEY,         -- tus upload id
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL DEFAULT 'file',
    mime        TEXT NOT NULL DEFAULT 'application/octet-stream',
    size        INTEGER NOT NULL DEFAULT 0,
    offset      INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'active',  -- active | completed | failed
    file_id     TEXT,                     -- set once finalised into files
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX idx_upload_sessions_status ON upload_sessions(status, updated_at);

CREATE TABLE shares (
    id           TEXT PRIMARY KEY,        -- ULID
    token_hash   TEXT NOT NULL UNIQUE,    -- sha-256 of the secret token
    file_id      TEXT NOT NULL REFERENCES files(id),
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER,                 -- NULL = never
    max_downloads INTEGER,                -- NULL = unlimited
    downloads    INTEGER NOT NULL DEFAULT 0,
    one_time     INTEGER NOT NULL DEFAULT 0,
    revoked_at   INTEGER
);
CREATE INDEX idx_shares_file ON shares(file_id);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Full-text index over the small text-bearing entities. Populated and kept in
-- sync by the application, not by triggers, so we control tokenisation and can
-- rebuild it on migration.
CREATE VIRTUAL TABLE search_fts USING fts5(
    entity UNINDEXED,   -- file | clipboard | snippet | note
    ref_id UNINDEXED,   -- the entity's primary key
    title,
    body,
    tokenize = 'unicode61 remove_diacritics 2'
);
