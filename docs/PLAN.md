---
type: plan
project: Wardline
created: 2026-10-28
updated: 2026-10-28
status: draft
---

# Wardline: Plan

Planning-level draft. Time estimates are rough and need real-world validation.

## 1. What it is
A private-network manager built on WireGuard for **non-technical people**. Set up a network, add a phone or laptop with a QR code, see who is connected, refresh security without thinking about it, and move to a new server with a guided wizard. Runs on Linux and Windows, with iPhone support in stages. Shown publicly as proof of what LSL can build and operate, and a possible Parcel module.

## 2. Decisions made
- **Greenfield.** No import or inference of any existing WireGuard setup. Everything comes from Wardline itself.
- **Non-technical first.** Plain vocabulary, one action per screen, live progress, technical detail hidden behind Advanced. Rules in `app/specs/UX.md`.
- **Develop in containers.** Docker Desktop only, no WSL distro or host Go install. Lab of containers rehearses server moves.
- **Name:** Wardline, repo `lsl-wardline`.
- **Hub-and-spoke.** Devices connect to a server, not to each other. No mesh or NAT traversal in v1.

## 3. Platform reality (see `app/specs/PLATFORMS.md`)
- **Servers:** Linux first (Docker or native). Windows server later. An iPhone cannot be a server.
- **Devices:** everything works in phase 1 using the official WireGuard apps and a code or file Wardline generates. Phase 2 adds silent agents for Linux and Windows. Phase 3 adds a native iPhone app (needs a Mac with Xcode and an Apple Developer account).
- **Admin app:** a responsive installable web app that works on an iPhone.

## 4. Architecture
One Go binary with roles: **control** (web app, API, database), **server** (the `gateway` role, applies WireGuard state), **agent** (device side, phase 2). SQLite database. Web UI in React, embedded in the binary. A `wg.Backend` interface with fake, userspace (`wireguard-go`) and Linux kernel implementations, so it runs in containers and on Windows.

Visibility comes from the server side (`last handshake`, traffic), so every device including an iPhone shows a status from day one.

## 5. Key lifecycle, in plain terms
- Keys are created for the user and hidden from them.
- Agent devices refresh their own keys silently on a schedule with an overlap window.
- Phones in phase 1 get a gentle "time to refresh" card and a new code; nothing is ever blocked.
- Removing access is immediate.

## 6. Moving to a new server
A wizard, one step per screen: set up the new server, test it, move everyone, watch progress, keep the old server for 24 hours, then finish. The strategy is chosen automatically: new server key when all devices are agents, sealed key transfer when manual devices exist. One-click rollback until the user finishes. Full state machine in `app/specs/BUSINESS-RULES.md`. A server address should be a name, not an IP, and the setup flow explains why.

## 7. Showcase and positioning
- Dogfood on LSL's own network first.
- Public case study: the plain-language design, the migration rehearsal in containers, the audit trail.
- Possible open-core split: agent and CLI open; managed features and Parcel integration paid.
- Compliance angle: Activity log answers who had access, from when to when, approved by whom.

## 8. Phases (rough)
| Phase | Scope |
|---|---|
| 1 | Usable product, no agents: setup wizard, add device, Home, remove access, Activity, container lab, e2e tests |
| 2 | Linux and Windows agents, one-click installers, silent refresh |
| 3 | Native iPhone app |
| 4 | Move-to-new-server wizard, control plane backup and restore |
| 5 | Windows server, invites, notifications, DNS integrations, docs and case study |

## 9. Risks
- Solo bandwidth. Keep v1 to hub-and-spoke.
- iOS app needs Apple tooling Dylan does not have yet.
- Control plane compromise equals network compromise: strong sign-in, signed state, least privilege.
- Crowded space: win on simplicity for non-technical users and on the audit trail, not on features.
- Clean-room separation from any employer work.
