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
