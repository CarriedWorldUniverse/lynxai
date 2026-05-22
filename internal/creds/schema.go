package creds

// schemaSQL is applied on Open to create tables if they don't exist.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS credentials (
  name        TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  host        TEXT NOT NULL,
  bundle      BLOB NOT NULL,
  nonce       BLOB NOT NULL,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS credential_audit (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT NOT NULL,
  used_at     INTEGER NOT NULL,
  request_url TEXT NOT NULL,
  outcome     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_name ON credential_audit(name);
`
