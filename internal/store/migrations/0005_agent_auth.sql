-- An agent device authenticates to the control plane with a bearer token. Only its hash is stored.
ALTER TABLE devices ADD COLUMN auth_token_hash TEXT;
CREATE UNIQUE INDEX devices_auth_token_hash ON devices(auth_token_hash) WHERE auth_token_hash IS NOT NULL;
