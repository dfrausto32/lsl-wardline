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
Need to know whether Dylan has a domain or subdomain (for example on laneysoftwarelogistics.com) to use as the server address for real testing. Not needed for the lab.

### Phase 3 prerequisites (not needed yet)
A Mac with Xcode (or a cloud Mac build service) and an Apple Developer Program membership, for the native iPhone app.

### Decisions
- Admin sign-in: password + authenticator app assumed for v1. Passkeys later if wanted.
- UI styling toolkit: undecided. Ask before choosing.
- Where the control plane will live in production: recommend a small separate host so a server move never takes the control plane with it. Same-host setups are allowed, with the limitation noted in BUSINESS-RULES.
- Single-tenant for now. Assumed unless Dylan says otherwise.
- Windows tunnel approach: decided at the start of phase 2.

## Spec notes (flagged for Dylan, no action needed unless you disagree)
- `devices.expires_at` added (BUSINESS-RULES "Expiry" needs it; DATA-MODEL.md does not list it). Task 1.1.
- `modernc.org/sqlite` pinned to v1.38.2: the latest (v1.61) requires Go 1.26, the dev image uses Go 1.24. Task 1.1.
- Git repo is on branch `master`; CLAUDE.md names `main`. Left as is; commits are on `master`.
- `servers.auth_token_hash` column added (migration 0003): a server needs a bearer token to talk to the control plane and DATA-MODEL.md has nowhere to keep its hash. Task 1.7.
- Install commands reference the image name `lsl-wardline:latest`. No registry is decided; tell me where images should be published (for example ghcr.io under an LSL account) before real use. Task 1.7.
- Not done in 1.7, on purpose: a used TOTP code can be replayed inside its 90-second window (no last-used-counter stored). Small, but noted.
- UX.md conflict (flagged, not silently resolved): flow 1 says the address field accepts "a name like `vpn.example.com`", but "VPN" is on the never-say list. The strings lint stays strict, so the UI example is `network.example.com`. Task 1.8.
- UX flow 2 mentions a "Send me a link" option for a person's own phone. Not built: it would require keeping the one-time code on the server until opened, which conflicts with "private keys stay on the device" and "shown once". Needs your decision. Task 1.9.
- UX flow 6 (Refresh security for phones) is not in the Phase 1 task table. The details page and Home show the gentle card after 180 days and its button opens "Add a device" for the same person; the old device is then removed by hand. A proper "new code replaces old code after first connection" flow is not built. Task 1.10.
