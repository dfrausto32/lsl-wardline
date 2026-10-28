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

### How the server is installed (decided: systemd)
