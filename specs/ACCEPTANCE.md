# Acceptance

## A. Phase 1 (all must be true)
1. `./scripts/verify.ps1` is green on a machine with only Docker Desktop installed. No WSL distro or host Go is required.
2. The backend contract suite passes on the fake and userspace implementations. The kernel implementation passes if the Docker Desktop kernel supports it, and the result is recorded either way.
3. Lab end-to-end passes: add a device, the device container connects with the generated config, the handshake is visible in the API and UI, remove access, traffic stops within 10 seconds.
4. Applying the same desired state twice yields an empty diff the second time.
5. `audit verify` passes on an untouched log and fails if any row is altered.
6. The no-private-keys test passes: no private key appears in the database, logs, API responses, or CLI output, except the one-time device code response, which is not stored.
7. The banned-word lint passes: no "never say" term from `specs/UX.md` appears in primary UI strings.
8. Playwright flows pass for: first-run setup, add device (all five types), remove access. Screenshots at 1280px and 375px widths were reviewed by Claude Code and show no overflow or clipped text.
9. Accessibility checks (axe via Playwright) report no serious or critical issues on the main screens.
10. `windows/amd64` and `linux/arm64` binaries cross-compile in the container.

## Manual checks (Dylan, on real hardware)
- M1. Add an iPhone using the official WireGuard app and a QR code. Time it. Target: under 3 minutes with no help.
- M2. Hand the setup and add-device flow to someone non-technical with no instructions. Note every place they hesitate. Each hesitation is a bug against `specs/UX.md`.
- M3. Add a Windows laptop with the downloaded file. Confirm it connects and shows as Connected.
- M4. Install the admin web app to the iPhone home screen and complete Home and Remove access one-handed.
- M5. Confirm a sleeping phone shows as "Not seen recently" with no alarm.

## B. Later phases
Written when each phase starts. Server move acceptance includes a rehearsal in the lab with two servers and a rollback, then a real-hardware rehearsal.
