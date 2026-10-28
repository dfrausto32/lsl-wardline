-- A server authenticates to the control plane with a bearer token. Only its hash is stored.
ALTER TABLE servers ADD COLUMN auth_token_hash TEXT;
CREATE UNIQUE INDEX servers_auth_token_hash ON servers(auth_token_hash) WHERE auth_token_hash IS NOT NULL;
