-- What an agent device reports about itself, and whether its last security refresh failed.
ALTER TABLE devices ADD COLUMN agent_reported_at TEXT;
ALTER TABLE devices ADD COLUMN agent_problem TEXT;
ALTER TABLE devices ADD COLUMN refresh_failed_at TEXT;
