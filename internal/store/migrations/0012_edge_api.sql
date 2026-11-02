-- The read-only API for the Edge (specs/MEMBERSHIP-AND-MODES.md section 9 and its addendum, section 16).

-- Keys other programs use to read from Wardline. Only a hash of the key is kept; the key is shown once.
CREATE TABLE api_tokens (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  scope TEXT NOT NULL,                 -- 'edge:read' is the only scope so far
  token_hash TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  last_used_at TEXT,
  revoked_at TEXT
);

-- Labels on a member. Wardline stores and returns them and never reads them.
CREATE TABLE member_labels (
  member_id TEXT NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY (member_id, key)
);

-- What changed, for the event stream. seq is the cursor. The newest 1000 are kept.
CREATE TABLE edge_events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  member_id TEXT NOT NULL,
  data TEXT NOT NULL,
  created_at TEXT NOT NULL
);

-- The member list as it was when changes were last looked for.
CREATE TABLE edge_snapshot (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  data TEXT NOT NULL
);
