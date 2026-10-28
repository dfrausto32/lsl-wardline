-- The last authenticator step that was accepted. A code from that step or an earlier one is never accepted again.
ALTER TABLE admin_users ADD COLUMN totp_last_counter INTEGER NOT NULL DEFAULT 0;
