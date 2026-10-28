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

- **Phase 2: device agents.** Linux and Windows agents, one-click installers, silent security refresh, device-side health, DNS re-resolve. Decide the Windows tunnel approach at the start.
- **Phase 3: iPhone app.** Native iOS app (Network Extension, WireGuardKit), scan-once enrollment, silent refresh. Needs a Mac with Xcode and an Apple Developer account.
- **Phase 4: move to a new server.** The wizard in UX flow 7, the state machine in BUSINESS-RULES, a rehearsal in the lab with two servers, plus control plane backup and restore.
- **Phase 5: reach.** Windows server, people invites (members add their own devices), email/push notifications, DNS provider integrations, public docs and case study.
