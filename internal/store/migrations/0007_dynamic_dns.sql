-- A free DuckDNS name that Wardline keeps pointed at this computer, for the one-computer setup.
-- The token is stored encrypted and never returned by the API.
CREATE TABLE dynamic_dns (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  provider TEXT NOT NULL CHECK (provider IN ('duckdns')),
  name TEXT NOT NULL,
  token_enc BLOB NOT NULL,
  last_ok_at TEXT,
  last_ip TEXT,
  last_error TEXT
);
