# wardline: build instructions for Claude Code

You are building `wardline` (repo name `lsl-wardline`), a private-network manager for Laney Software Logistics (LSL), built on WireGuard. The people who use it are **not technical**. They should be able to set up a private network, add a phone or laptop, see who is connected, and move to a new server without knowing what a key, a subnet, or a handshake is. All the WireGuard detail still exists underneath, but it stays out of the way.

**Read before writing any code, in this order:** `specs/UX.md` (who this is for and how every screen must behave), `specs/PLATFORMS.md` (what runs where), `specs/BUILD-ORDER.md` (task order and definition of done), `specs/DATA-MODEL.md`, `specs/BUSINESS-RULES.md` (key lifecycle, status, migration: the rules most likely to be got wrong), `specs/ACCEPTANCE.md`, `docs/DEV-ENVIRONMENT.md` (container setup). Background: `docs/PLAN.md`.

**Precedence when sources conflict:** `specs/UX.md` for anything the user sees, then `specs/BUSINESS-RULES.md`, then `specs/DATA-MODEL.md`, then `docs/PLAN.md`. Flag conflicts rather than silently picking one.

## Non-negotiables

1. **Non-technical users first.** Every screen, message, and error follows `specs/UX.md`. No WireGuard jargon in the primary UI. Reason: the product's whole value is that someone with no networking knowledge can run it. A test fails the build if banned words appear in primary UI strings.
2. **Greenfield only.** Wardline never imports, scans, or infers an existing WireGuard setup. Everything it knows was created through Wardline. Reason: Dylan's decision. Inference produces guesswork and support burden.
3. **Private keys stay on the device that owns them.** The control plane stores public keys only. Two labelled exceptions: (a) a "manual" device such as an iPhone using the official WireGuard app gets a one-time keypair, shown once as a QR code or file, then discarded; (b) moving a server's key to a new server uses a sealed transfer that the control plane cannot read (see BUSINESS-RULES). Both are audited. A test scans the database and logs for private keys.
4. **Tests first, then code.** Every task names its test file. Never modify a test to make it pass. If a test is wrong, say so and stop.
5. **All WireGuard operations go through the `wg.Backend` interface**, with three implementations: in-memory fake (unit tests), userspace (`wireguard-go`, works in any container and on any OS), and Linux kernel (`wgctrl` + `netlink`, build-tagged). One contract test suite runs against every implementation. Reason: dev happens in containers where the kernel module may not exist, and the product must run on Windows too.
6. **The database is the source of truth.** Configs are rendered, never hand-edited. Applying state is declarative and idempotent: diff against live state and change only the difference. Running apply twice changes nothing.
7. **Every change writes an audit event** through the single helper in `internal/audit`. The log is append-only and hash-chained, with a verify command. It is shown to users as "Activity".
8. **No secrets in logs, errors, or API responses.** Use the redacting logger. Preshared keys and TOTP secrets are encrypted at rest.
9. **Server moves are reversible until the user explicitly finishes them.** Never stop or delete the old server automatically.
10. **Hub-and-spoke only in v1.** Devices connect to a server; devices do not connect to each other. No mesh, no NAT traversal.
11. **Clean-room separation.** Built on Dylan's personal time and equipment, independent of any employer work. Do not import, reference, or reproduce designs, code, or documents from outside this repo. If unsure whether something originated elsewhere, stop and ask.
12. **Boring code, one binary.** A single static Go binary `wardline` with subcommands (`control`, `gateway`, `agent`, plus admin CLI). `CGO_ENABLED=0`. Standard library first, few dependencies. This is maintained by a solo operator.
13. **Develop in containers.** Nothing may require WSL or a host-installed Go toolchain. The only host requirement is Docker Desktop. All build, test, and lab commands run through `docker compose`. See `docs/DEV-ENVIRONMENT.md`.

## Environment

Dylan develops on **Windows 11 with PowerShell and Docker Desktop**. The repo lives at `C:\Users\fraus\Claude\Projects\LSL\01 Projects\Wardline\app`.

- **The repo path contains spaces.** Quote paths in PowerShell. Do not relocate the repo to fix a tool. Inside containers the repo is mounted at `/src`, so there are no spaces there.
- **Line endings:** `.gitattributes` forces LF. Do not change `core.autocrlf` or commit CRLF.
- **No sandboxing** for Claude Code on native Windows. Commands run with Dylan's permissions. Nothing destructive outside the repo. `docker system prune` and similar are Dylan's call, never yours.
- PowerShell's execution policy can block `.ps1` scripts. Use `powershell -ExecutionPolicy Bypass -File scripts/<name>.ps1`.
- Docker Desktop runs Linux containers. Windows and mobile code is cross-compiled in the container and cannot be run there. Things that need a real Windows machine or an iPhone are listed in `specs/ACCEPTANCE.md` as manual checks; add them to `BLOCKED.md` when you reach them.

## Commands (run from the `app` directory in PowerShell)

```powershell
./scripts/verify.ps1      # vet + unit tests + UI tests + string lint, all in containers. Run before declaring a task done.
./scripts/lab-up.ps1      # start the lab: control plane, two servers, simulated devices on a private network
./scripts/lab-down.ps1    # stop the lab, keep data
./scripts/e2e.ps1         # end-to-end tests against the lab
docker compose -f compose.dev.yaml run --rm dev go test ./...     # raw form of the unit tests
```

## Stack

Decided, do not substitute: Go 1.23 or newer · SQLite via `modernc.org/sqlite` · `golang.zx2c4.com/wireguard` (userspace backend) · `golang.zx2c4.com/wireguard/wgctrl` · `github.com/vishvananda/netlink` (Linux only) · `github.com/spf13/cobra` · `net/http` stdlib mux · embedded SQL migrations · React + TypeScript + Vite for the web UI, built into the binary with `go:embed` · Playwright for UI tests (in the official Playwright container) · the UI is a responsive, installable web app (PWA) that works on an iPhone screen.

Undecided, ask before choosing: UI styling toolkit, Windows tunnel approach (phase 2), admin sign-in beyond password + TOTP, email/push notification provider, DNS provider integrations.

## Layout

`cmd/wardline`, `internal/{store,audit,ipam,render,wg,gateway,agent,api,auth,policy,migrate,log}`, `web/` (UI source), `testdata/`, `scripts/`, `compose.dev.yaml`, `Dockerfile.dev`, `Dockerfile`.

## Working style

- One BUILD-ORDER task at a time, in order. Run `./scripts/verify.ps1` at the end of each. Commit per task with the task ID in the message.
- When blocked on something only Dylan can do, write it to `BLOCKED.md` with what you need and move on to the next unblocked task.
- Report what you verified, not what you inferred. "Not yet tested" is not "ruled out": say which you mean.
- When you build a screen, look at it (Playwright screenshot at desktop and iPhone widths) before calling it done.
- If you are about to build something that is not in the specs, stop and ask. Scope creep is the main risk for a solo project.
