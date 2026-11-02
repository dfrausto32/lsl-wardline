-- Membership log (specs/MEMBERSHIP-AND-MODES.md section 11). No private key is stored here or anywhere in this schema:
-- entries hold public keys and signatures only.

CREATE TABLE groups (
  id TEXT PRIMARY KEY,                 -- the id of the genesis entry
  name TEXT NOT NULL,
  mode TEXT NOT NULL CHECK (mode IN ('authority', 'open', 'admins_k_of_n')),
  genesis_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE membership_entries (
  id TEXT PRIMARY KEY,                 -- hex SHA-256 of the signed bytes
  group_id TEXT NOT NULL REFERENCES groups(id),
  author TEXT NOT NULL,
  type TEXT NOT NULL,
  body BLOB NOT NULL,                  -- the whole entry as canonical JSON
  sig BLOB NOT NULL,
  depth INTEGER NOT NULL,
  received_at TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('applied', 'queued', 'rejected')),
  reject_reason TEXT
);
CREATE INDEX membership_entries_group ON membership_entries(group_id, depth);

-- A projection of the log, rewritten whenever it changes. Never edited by hand.
CREATE TABLE members (
  group_id TEXT NOT NULL REFERENCES groups(id),
  member_id TEXT NOT NULL,
  name TEXT NOT NULL,
  hostname_label TEXT,
  signing_pubkey TEXT,
  wireguard_pubkey TEXT,
  overlay_address TEXT,
  roles TEXT NOT NULL,                 -- comma separated: admin, relay, gateway, voter
  status TEXT NOT NULL CHECK (status IN ('active', 'removed')),
  added_by TEXT,
  added_entry TEXT,
  removed_entry TEXT,
  PRIMARY KEY (group_id, member_id)
);
