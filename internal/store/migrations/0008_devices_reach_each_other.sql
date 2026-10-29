-- Network setting: whether the server relays traffic between devices. Off by default (devices isolated).
ALTER TABLE networks ADD COLUMN devices_reach_each_other INTEGER NOT NULL DEFAULT 0 CHECK (devices_reach_each_other IN (0, 1));
