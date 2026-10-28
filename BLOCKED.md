# Blocked

Things Claude Code cannot do alone. Format: what's needed, who from, what was done instead.
Clear an entry by deleting it once the input lands.

---

## Open

### Kernel WireGuard in the Docker Desktop VM (information, not a blocker)
Checked in task 1.0 on 2026-10-28 (Docker 29.6.2, Docker Desktop VM): `ip link add ... type wireguard` SUCCEEDS with NET_ADMIN. The kernel backend can run in the lab and in the contract tests. The userspace backend is still built and tested (required on Windows and in hosts without the module).

### Manual checks need real hardware
M1 to M5 in `specs/ACCEPTANCE.md` need Dylan, an iPhone, a Windows machine, and ideally one non-technical tester.

### Server address for real use
Dylan owns laneysoftwarelogistics.com but has no domain set aside for this. Plan: add a subdomain such as `network.laneysoftwarelogistics.com` with an A record pointing at the server's public IP, at whoever hosts the domain's DNS. Needs Dylan, once a real server exists. Not needed for the lab.

### How the server is installed (decision pending)
Installs are currently documented as a container image (`lsl-wardline:latest`, no registry). Dylan asked whether systemd-style services could be used instead. The binary is a single static file, so a systemd unit is a natural fit for a plain Linux server. Waiting on Dylan's choice before changing `docs/INSTALL.md` or adding anything to the repo.

### Phase 3 prerequisites (not needed yet)
A Mac with Xcode (or a cloud Mac build service) and an Apple Developer Program membership, for the native iPhone app.

### Decisions
- Admin sign-in: password + authenticator app assumed for v1. Passkeys later if wanted.
- UI styling toolkit: plain CSS (decided).
- Where the control plane will live in production: recommend a small separate host so a server move never takes the control plane with it. Same-host setups are allowed, with the limitation noted in BUSINESS-RULES.
- Single-tenant for now. Assumed unless Dylan says otherwise.
- Windows tunnel approach: decided at the start of phase 2.
- Git branch stays `master` (decided).
- "Send me a link" for phones (UX flow 2): left out of v1 (decided). If wanted later it needs a sealed, single-use, short-lived handoff, specified first.
- Refresh security for phones (UX flow 6): the Phase 1 behaviour (gentle card, button opens "Add a device") is accepted. A replace-after-first-connection flow goes on the Phase 2 task list.

## Spec notes (resolved, kept for the record)
- `devices.expires_at`, `servers.auth_token_hash` and `admin_users.totp_last_counter` are now in `specs/DATA-MODEL.md`.
- `specs/UX.md` flow 1 now uses `network.example.com` as its example address (the old one contained a banned word).
- Go and SQLite bumped: dev and production images use Go 1.26, `modernc.org/sqlite` is v1.61.0.
- Authenticator codes can no longer be reused (migration 0004). Two existing tests were changed to wait one 30-second step after switching two-step on, because the code used to enable it is now spent. This is the intended behaviour, not a weakening of the tests.
- Install commands still use the placeholder image name `lsl-wardline:latest` until the install-method decision above is made.
