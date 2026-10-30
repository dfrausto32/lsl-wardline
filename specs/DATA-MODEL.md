# Data model (SQLite)

Times are UTC, stored as RFC 3339 text. IDs are ULIDs as text. No private key column exists anywhere. Internal names are technical; the UI uses the vocabulary in `specs/UX.md`.

- **networks**: id, name, address_range (managed automatically, never typed by a user), server_address (the name or IP devices use), server_address_kind (`name` | `ip`), devices_reach_each_other (0/1, default 0; migration 0008), created_at
- **servers**: id, network_id, name, public_key, listen_port, role (`active` | `standby` | `retiring` | `retired`), last_seen_at, created_at, retired_at, auth_token_hash (the server's bearer token, hash only)
- **people**: id, network_id, display_name, created_at. No longer shown or edited: Wardline has machines, not people. Machines made without a person hang off one hidden row named "Machines", and older rows are left as they are.
- **devices**: id, network_id, person_id, name, platform (`ios` | `android` | `windows` | `macos` | `linux` | `other`), kind (`manual` | `agent`), tunnel_ip, status (`waiting` | `active` | `removed`), added_at, removed_at, last_seen_at, last_endpoint, rx_bytes, tx_bytes, agent_version, refresh_due_at, expires_at (optional access end date), auth_token_hash (agent devices only; the token itself is never stored)
- **device_keys**: id, device_id, public_key, state (`pending` | `active` | `retired`), created_at, activated_at, retired_at. At most one `active` and one `pending` per device.
- **preshared_keys**: device_id, ciphertext, nonce, created_at. Encrypted at rest with a key from the environment or a key file, never stored in the database.
- **enrollment_tokens**: id, device_id or server_id, token_hash, expires_at, used_at
- **ip_allocations**: network_id, ip, device_id (nullable), state (`reserved` | `allocated` | `quarantined`), released_at
- **admin_users**: id, email, password_hash (argon2id), totp_secret_ciphertext, totp_last_counter (last accepted authenticator step; a code is never accepted twice), created_at
- **sessions**: id, admin_user_id, token_hash, created_at, expires_at
- **audit_events**: id, ts, actor, action, subject_type, subject_id, plain_text (the human sentence shown in Activity), details (JSON, redacted), prev_hash, hash
- **server_moves**: id, network_id, from_server_id, to_server_id, state (see BUSINESS-RULES), key_strategy (`new_key` | `carry_key`), started_at, finished_at, report (JSON)
- **settings**: key, value (non-secret app settings)

Indexes: unique (network_id, name) on devices, unique (network_id, tunnel_ip) on allocated addresses, unique public_key across active device keys.

## Names for machines (added 2026-10-30, migration 0010)
- **devices**: `short_name` (lowercase name made from `name`, unique per network among machines that are not removed), `short_name_custom` (0/1, default 0: 1 when the person set it by hand).
- **device_aliases**: id, device_id, network_id, name (unique per network), created_at. Deleted with the machine's names when it is removed.
- No setting of its own: names answer exactly when `networks.devices_reach_each_other` is 1.
