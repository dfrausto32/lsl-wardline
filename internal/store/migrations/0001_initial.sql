-- Schema from specs/DATA-MODEL.md. Times are UTC RFC 3339 text, IDs are ULIDs.
-- No private key column exists anywhere.

CREATE TABLE networks (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  address_range TEXT NOT NULL,
  server_address TEXT NOT NULL,
  server_address_kind TEXT NOT NULL CHECK (server_address_kind IN ('name','ip')),
  created_at TEXT NOT NULL
);

CREATE TABLE servers (
  id TEXT PRIMARY KEY,
  network_id TEXT NOT NULL REFERENCES networks(id),
  name TEXT NOT NULL,
  public_key TEXT NOT NULL,
  listen_port INTEGER NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('active','standby','retiring','retired')),
  last_seen_at TEXT,
  created_at TEXT NOT NULL,
  retired_at TEXT
);

CREATE TABLE people (
  id TEXT PRIMARY KEY,
  network_id TEXT NOT NULL REFERENCES networks(id),
  display_name TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE devices (
  id TEXT PRIMARY KEY,
  network_id TEXT NOT NULL REFERENCES networks(id),
  person_id TEXT NOT NULL REFERENCES people(id),
  name TEXT NOT NULL,
  platform TEXT NOT NULL CHECK (platform IN ('ios','android','windows','macos','linux','other')),
  kind TEXT NOT NULL CHECK (kind IN ('manual','agent')),
  tunnel_ip TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('waiting','active','removed')),
  added_at TEXT NOT NULL,
  removed_at TEXT,
  last_seen_at TEXT,
  last_endpoint TEXT,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  agent_version TEXT,
  refresh_due_at TEXT,
  expires_at TEXT
);
CREATE UNIQUE INDEX devices_name_per_network ON devices(network_id, name);
-- A removed device's address stays held by ip_allocations (quarantine), so the
-- uniqueness enforced here is among devices that still exist.
CREATE UNIQUE INDEX devices_live_tunnel_ip ON devices(network_id, tunnel_ip) WHERE status <> 'removed';

CREATE TABLE device_keys (
  id TEXT PRIMARY KEY,
  device_id TEXT NOT NULL REFERENCES devices(id),
  public_key TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('pending','active','retired')),
  created_at TEXT NOT NULL,
  activated_at TEXT,
  retired_at TEXT
);
CREATE UNIQUE INDEX device_keys_active_public_key ON device_keys(public_key) WHERE state = 'active';
CREATE UNIQUE INDEX device_keys_one_active ON device_keys(device_id) WHERE state = 'active';
CREATE UNIQUE INDEX device_keys_one_pending ON device_keys(device_id) WHERE state = 'pending';

CREATE TABLE preshared_keys (
  device_id TEXT PRIMARY KEY REFERENCES devices(id),
  ciphertext BLOB NOT NULL,
  nonce BLOB NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE enrollment_tokens (
  id TEXT PRIMARY KEY,
  device_id TEXT REFERENCES devices(id),
  server_id TEXT REFERENCES servers(id),
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  used_at TEXT,
  CHECK ((device_id IS NULL) <> (server_id IS NULL))
);

CREATE TABLE ip_allocations (
  network_id TEXT NOT NULL REFERENCES networks(id),
  ip TEXT NOT NULL,
  device_id TEXT REFERENCES devices(id),
  state TEXT NOT NULL CHECK (state IN ('reserved','allocated','quarantined')),
  released_at TEXT,
  PRIMARY KEY (network_id, ip)
);

CREATE TABLE admin_users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  totp_secret_ciphertext BLOB,
  created_at TEXT NOT NULL
);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  admin_user_id TEXT NOT NULL REFERENCES admin_users(id),
  token_hash TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE TABLE audit_events (
  id TEXT PRIMARY KEY,
  ts TEXT NOT NULL,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  subject_type TEXT NOT NULL,
  subject_id TEXT NOT NULL,
  plain_text TEXT NOT NULL,
  details TEXT NOT NULL DEFAULT '{}',
  prev_hash TEXT NOT NULL,
  hash TEXT NOT NULL
);

CREATE TABLE server_moves (
  id TEXT PRIMARY KEY,
  network_id TEXT NOT NULL REFERENCES networks(id),
  from_server_id TEXT NOT NULL REFERENCES servers(id),
  to_server_id TEXT NOT NULL REFERENCES servers(id),
  state TEXT NOT NULL,
  key_strategy TEXT NOT NULL CHECK (key_strategy IN ('new_key','carry_key')),
  started_at TEXT NOT NULL,
  finished_at TEXT,
  report TEXT
);

CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
