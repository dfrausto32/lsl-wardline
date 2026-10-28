# Blocked

Things Claude Code cannot do alone. Format: what's needed, who from, what was done instead.
Clear an entry by deleting it once the input lands.

---

## Open

### Kernel WireGuard in the Docker Desktop VM (information, not a blocker)
Checked in task 1.0 on 2026-10-28 (Docker 29.6.2, Docker Desktop VM): `ip link add ... type wireguard` SUCCEEDS with NET_ADMIN. The kernel backend can run in the lab and in the contract tests. The userspace backend is still built and tested (required on Windows and in hosts without the module).

### Manual checks need real hardware
M1 to M6 in `specs/ACCEPTANCE.md` need Dylan, an iPhone, a Windows machine, and ideally one non-technical tester.

### Server address for real use
Dylan owns laneysoftwarelogistics.com but has no domain set aside for this. Plan: add a subdomain such as `network.laneysoftwarelogistics.com` with an A record pointing at the server's public IP, at whoever hosts the domain's DNS. Needs Dylan, once a real server exists. Not needed for the lab.

### How the server is installed (decided: systemd)
Decided: systemd is the primary install on Linux (`deploy/`, `docs/INSTALL.md`); containers stay as the alternative. Still manual: nothing was tested against a live systemd (the unit files for the server, control plane and agent pass `systemd-analyze verify` and the install script was run with a stubbed `systemctl`), so the first real install on a Linux host is a manual check. No public registry is needed.

### Phase 2: things only Dylan can do
- **GitHub repository:** one is needed before installers can be published as GitHub Releases (`scripts/release.ps1` builds everything and prints the `gh release create` command). Account and name are Dylan's call.
- **Windows installer and WireGuard:** `wardline.exe agent install` does NOT download WireGuard for Windows; it stops with a plain message and a link if it is missing. Reason: the SHA-256 for `wireguard-amd64-1.1.1.msi` in WireGuard's own `latest.sig` did not match the file I downloaded from the same server (f64b88de... listed, 7bfed60a... received), and I could not verify the signature here. Dylan to decide whether to bundle or fetch it later, after checking the licence and verifying the signature. See `docs/WINDOWS-TUNNEL.md`.
- **A real Windows machine** for manual check M6 (agent install, connects, survives a reboot, removed device loses access). Also the first real Linux install with systemd.

### Phase 3 prerequisites (DEFERRED INDEFINITELY by Dylan, 2026-10-28; ignore)
A Mac with Xcode (or a cloud Mac build service) and an Apple Developer Program membership, for the native iPhone app.

### Decisions
- Admin sign-in: password + authenticator app assumed for v1. Passkeys later if wanted.
- UI styling toolkit: plain CSS (decided).
- Where the control plane will live in production: recommend a small separate host so a server move never takes the control plane with it. Same-host setups are allowed, with the limitation noted in BUSINESS-RULES.
- Single-tenant for now. Assumed unless Dylan says otherwise.
- Windows tunnel approach: drive the official WireGuard for Windows (decided 2026-10-28). Installers on GitHub Releases (decided).
- Git branch stays `master` (decided).
- "Send me a link" for phones (UX flow 2): left out of v1 (decided). If wanted later it needs a sealed, single-use, short-lived handoff, specified first.
- Refresh security for phones (UX flow 6): the Phase 1 behaviour (gentle card, button opens "Add a device") is accepted. A replace-after-first-connection flow goes on a later task list.

## Phase 2 spec notes (flagged for Dylan, no action needed unless you disagree)
- BUSINESS-RULES says an agent sends its new key "signed with the current key" and both keys work for an overlap window. WireGuard keys cannot sign (they are X25519), so the request is authenticated with the device's own access token instead. And WireGuard allows only one key per tunnel address, so two keys cannot work at once. What was built: the control plane swaps keys the moment the agent asks, the agent checks the new key really connects (30 s), and if not it asks the control plane to go back. The old key is kept for an hour as the way back. A failed refresh shows an attention card and is retried a day later. Tested by unit tests on both sides and a lab run where traffic kept flowing through a refresh.
- UX flow 2 says Phase 2 replaces the file with a "one-click installer". Built: one command per computer (Linux install script, Windows `agent install`). A graphical Windows installer (MSI/EXE with a button) is not built; it needs packaging tooling and the WireGuard bundling decision above.
- Windows keys are protected by a folder only the system and administrators can read, not by Windows DPAPI. DPAPI would need more code and cannot be tested without a Windows machine. Linux keys are in a 0600 file owned by a dedicated service user.
- Server-address re-lookup (2.4) is proven by unit tests with a fake resolver. The lab has no second server until Phase 4, so a real address change was not run end to end.
- The Windows pieces (service, WireGuard-for-Windows driver, `agent install`, `agent uninstall`) compile for windows/amd64 and their command order is unit tested with the WireGuard programs stubbed, but nothing was run on Windows.
- Added: `iputils-ping` in the production image (lab agents use it), `golang.org/x/sys` as a direct dependency (already in the build), migrations 0005 and 0006, and `wardline agent refresh` (asks a running agent to refresh now).
- One mistake of mine, fixed: an earlier edit to this file truncated it and was committed (5629cf7). This version was rebuilt from the previous good copy plus the Phase 2 entries.

## Spec notes (resolved, kept for the record)
- `devices.expires_at`, `servers.auth_token_hash` and `admin_users.totp_last_counter` are now in `specs/DATA-MODEL.md`. Agent columns (`auth_token_hash` on devices, `agent_reported_at`, `agent_problem`, `refresh_failed_at`) are added in migrations 0005 and 0006; DATA-MODEL.md lists `devices.auth_token_hash`.
- `specs/UX.md` flow 1 now uses `network.example.com` as its example address (the old one contained a banned word).
- Go and SQLite bumped: dev and production images use Go 1.26, `modernc.org/sqlite` is v1.61.0.
- Authenticator codes can no longer be reused (migration 0004). Two existing tests were changed to wait one 30-second step after switching two-step on, because the code used to enable it is now spent. This is the intended behaviour, not a weakening of the tests.
- The container install still uses the placeholder image name `lsl-wardline:latest`, built locally; no registry is needed for the systemd install.
