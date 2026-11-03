-- Reach-all permission (specs/BUSINESS-RULES.md, "Reach-all permission"): a machine that may reach every other machine
-- and be reached by every other machine through the server, while the network setting is off. Off by default.
ALTER TABLE devices ADD COLUMN can_reach_all INTEGER NOT NULL DEFAULT 0 CHECK (can_reach_all IN (0, 1));
