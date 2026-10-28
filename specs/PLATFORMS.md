# Platforms

Wardline has three roles: **control** (the web app and brain), **server** (the machine devices connect to; the binary calls this the `gateway` role), and **device** (phones, laptops, anything that connects).

| Platform | Server | Device (phase 1: manual setup) | Device (agent, silent refresh) | Admin web app |
|---|---|---|---|---|
| Linux (incl. Docker) | First class. Docker image and native binary. | Yes | Phase 2 | Yes |
| Windows | Later (phase 5). Needs routing and NAT setup that differs from Linux; do not start before Linux is accepted. | Yes | Phase 2 | Yes |
| iPhone | **Not possible.** iOS cannot accept inbound connections or run a background server. | Yes, via the official WireGuard app and a QR code | Phase 3: native Wardline iOS app | Yes (installable web app) |
| Android | Not possible | Yes, via the official WireGuard app and a QR code | Later | Yes |
| macOS | Not in scope | Yes, via the official WireGuard app | Not in scope | Yes |

## What "works on iPhone" means
1. **Phase 1:** the admin web app runs well on an iPhone and installs to the home screen. An iPhone connects as a device using the official WireGuard app. Wardline shows its status and last seen from the server side. Refreshing its security needs the user to scan a new code.
2. **Phase 3:** a native Wardline iOS app built with the Network Extension framework and WireGuardKit. It enrolls by scanning one code, keeps the private key in the device keychain, refreshes security silently, and reports status. This needs a Mac with Xcode (or a cloud Mac build service) and an Apple Developer Program membership. See `BLOCKED.md`.

## Windows notes
- The Windows device agent runs as a Windows service. The tunnel approach (drive the official WireGuard for Windows tunnel service, or embed `wireguard-go` with the Wintun driver) is decided at the start of phase 2 after reading current docs and licenses.
- Containers cannot run Windows code. Cross-compile with `GOOS=windows` in the container; Dylan runs the manual Windows checks.

## Server notes (Linux)
- Needs UDP port reachable from the internet, IP forwarding, and NAT rules. The `server` role sets these up itself and tells the user in plain words if the host blocks it ("Your host isn't allowing the connection port. Here's what to open.").
- One-command install must work for: a container on any Docker host, and a plain Linux machine.
