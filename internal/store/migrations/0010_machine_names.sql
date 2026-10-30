-- Names for machines (specs/BUSINESS-RULES.md, "Names for machines").
-- short_name is made from the machine's name; short_name_custom is 1 when the person typed it by hand.
-- Machines made before this get their short names when the control plane starts.
ALTER TABLE devices ADD COLUMN short_name TEXT;
ALTER TABLE devices ADD COLUMN short_name_custom INTEGER NOT NULL DEFAULT 0 CHECK (short_name_custom IN (0, 1));
-- What the machine itself says about using names: 'ok' or 'unavailable'.
ALTER TABLE devices ADD COLUMN agent_names TEXT;

CREATE TABLE device_aliases (
  id TEXT PRIMARY KEY,
  device_id TEXT NOT NULL REFERENCES devices(id),
  network_id TEXT NOT NULL,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (network_id, name)
);
CREATE INDEX device_aliases_device ON device_aliases(device_id);
