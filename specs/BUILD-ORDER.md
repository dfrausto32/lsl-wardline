# Build order

Work in order. Each task lists its test file; write the test first. Run `./scripts/verify.ps1` at the end of each task. Build UI tasks with Playwright screenshots at desktop and iPhone width (375px) and look at them.

## Phase 1: a usable product, no device agents

Everything a non-technical person needs: set up a network, add phones and computers, see who is connected, remove access. Devices use the official WireGuard apps with codes or files that Wardline generates. Visibility comes from the server side, so it works for every device including iPhones.

| ID | Task | Test file |
|---|---|---|
| 1.0 | Container dev environment: `Dockerfile.dev`, `compose.dev.yaml`, `scripts/*.ps1`, kernel WireGuard check, record result in `BLOCKED.md` as information | `scripts/smoke.ps1` plus `internal/wg/env_test.go` |
| 1.1 | Redacting logger, SQLite store, embedded migrations for `specs/DATA-MODEL.md` | `internal/log/log_test.go`, `internal/store/store_test.go` |
| 1.2 | Audit log: append, hash chain, verify, plain-text sentences | `internal/audit/audit_test.go` |
| 1.3 | Addressing: automatic range choice, allocation, quarantine | `internal/ipam/ipam_test.go` |
| 1.4 | `wg.Backend` interface, in-memory fake, userspace implementation, Linux kernel implementation (build-tagged), one shared contract suite, diff computation | `internal/wg/contract_test.go`, `internal/wg/diff_test.go` |
| 1.5 | Renderer: server config and device config (QR code and file) from DB state, golden files | `internal/render/render_test.go` |
| 1.6 | Server role (`wardline gateway`): enroll to control plane, pull signed desired state, apply idempotently, report per-device last handshake and traffic | `internal/gateway/gateway_test.go` |
| 1.7 | API (REST, JSON) and admin auth: password (argon2id), optional TOTP, sessions, rate limiting. No-private-keys scan test | `internal/api/api_test.go`, `internal/auth/auth_test.go`, `internal/api/noprivatekeys_test.go` |
| 1.8 | UI shell, installable web app, plain-language string file with banned-word lint, first-run setup wizard (UX flow 1) | `web/tests/setup.spec.ts`, `web/tests/strings.test.ts` |
| 1.9 | UI: Add a device (UX flow 2) for iPhone, Android, Windows, Mac, Linux, with live waiting-then-connected | `web/tests/add-device.spec.ts` |
| 1.10 | UI: Home, device details, remove access, Activity (UX flows 3, 4, 5, 8) | `web/tests/home.spec.ts`, `web/tests/remove.spec.ts` |
| 1.11 | Lab end-to-end: control + server + two simulated devices. Add a device via the API, device container connects with the generated config, handshake appears, remove access, traffic stops | `e2e/lab_test.go` via `scripts/e2e.ps1` |
| 1.12 | Production image and one-command server install docs. Cross-compile check for `windows/amd64` and `linux/arm64` | `scripts/crosscompile.ps1` |

Phase 1 is done when `specs/ACCEPTANCE.md` section A passes. Then stop and wait for Dylan.

## Later phases (task tables written when each phase starts)

- **Phase 2: device agents.** Linux and Windows agents, one-click installers, silent security refresh, device-side health, DNS re-resolve. Decide the Windows tunnel approach at the start. Task table below (PROPOSED 2026-10-28, not yet approved by Dylan; do not start 2.1 until the Windows tunnel decision in 2.0 is made).
- **Phase 3: iPhone app. DEFERRED INDEFINITELY (Dylan, 2026-10-28). Do not plan or start it.** Native iOS app (Network Extension, WireGuardKit), scan-once enrollment, silent refresh. Needs a Mac with Xcode and an Apple Developer account.
- **Phase 4: move to a new server.** The wizard in UX flow 7, the state machine in BUSINESS-RULES, a rehearsal in the lab with two servers, plus control plane backup and restore.
- **Phase 5: reach.** Windows server, people invites (members add their own devices), email/push notifications, DNS provider integrations, public docs and case study.

## Phase 2 task table (proposed)

Goal: a computer joins with one click, keeps its own private key, refreshes it silently, and reports its own health. Linux and Windows agents. Same rules as Phase 1: test first, `verify.ps1` at the end, commit per task.

| ID | Task | Test file |
|---|---|---|
| 2.0 | DONE 2026-10-28: Windows tunnel = drive official WireGuard for Windows; installers on GitHub Releases. See `docs/WINDOWS-TUNNEL.md` | none (decision record) |
| 2.1 | Agent enrollment API: one-time link, device generates its own keypair and sends only the public key; agent pulls signed desired state; extend the no-private-keys scan to agent devices | `internal/api/agent_test.go`, `internal/api/noprivatekeys_test.go` |
| 2.2 | Agent core (`wardline agent`): key kept in OS-protected storage, apply via `wg.Backend` idempotently, report health | `internal/agent/agent_test.go` |
| 2.3 | Silent security refresh: agent rotates its key, control swaps keys without a gap, old key retired (BUSINESS-RULES key lifecycle) | `internal/agent/refresh_test.go`, `internal/api/refresh_test.go` |
| 2.4 | Server-address re-lookup: agents follow a changed name or address (groundwork for Phase 4) | `internal/agent/resolve_test.go` |
| 2.5 | Device-side health in the API and on Home/details (which side reports what; plain-language states) | `internal/api/health_test.go`, `web/tests/home.spec.ts` |
| 2.6 | Linux agent: systemd unit, one-line install, lab devices switch to agents | `e2e/agent_lab_test.go` via `scripts/e2e.ps1` |
| 2.7 | Windows agent: service, installer, cross-compile check (needs a real Windows machine for the manual check) | `scripts/crosscompile.ps1`, manual M-check |
| 2.8 | UI: Add a device for Windows and Linux becomes the one-click installer; the 180-day card goes away for agent devices (UX flows 2 and 6) | `web/tests/add-device.spec.ts` |
| 2.9 | Release build and where installers are published | `scripts/release.ps1` |

Phase 2 is done when the agent items in `specs/ACCEPTANCE.md` pass (to be added with the table) and the Windows manual check is recorded. Then stop and wait for Dylan.

### Phase 2 status (2026-10-28)
2.0 to 2.9 are built and committed; tests pass in the container and the lab. Not done and needing Dylan: the Windows pieces have not run on Windows (manual check M6), and publishing to GitHub Releases waits for a repository. Details and every deviation from the specs are in `BLOCKED.md` ("Phase 2 spec notes"). Phase 3 is deferred indefinitely.
