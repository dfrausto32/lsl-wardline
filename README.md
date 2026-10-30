# Wardline

A private network for people who don't know networking, built on [WireGuard](https://www.wireguard.com/).

Set up a network, add a phone or a computer, see what is connected, and move to a new server, without ever
seeing a key, a subnet or a handshake. The WireGuard details are all still there underneath; they just stay
out of the way. Wardline is made by Laney Software Logistics (LSL).

## What you get

- **One program.** `wardline` is a single file with no other software to install. On Windows it carries its own
  tunnel driver, so there is no second app.
- **A web app** for the person running the network: add a machine by kind and name, see who is connected in
  plain words, rename or remove a machine, read the Activity log.
- **A desktop window and tray icon** on Windows computers that have joined, like the WireGuard app's.
- **Short commands** on joined computers: `wardline on`, `wardline off`, `wardline status`.
- **Private keys stay on the machine that owns them.** The server stores public keys only. (Phones using the
  official WireGuard app get a one-time key, shown once as a QR code, then discarded.)
- **Machines can reach each other** if you turn that on (it is off until you do).
- **Servers that update themselves** from releases signed with your own key. See [docs/AUTO-UPDATE.md](docs/AUTO-UPDATE.md).

## Get it

Download from the [Releases](https://github.com/dfrausto32/lsl-wardline/releases/latest) page.

| You want to | Use |
| --- | --- |
| Run the web app and server on a Windows PC | Download `wardline.exe` and double-click it |
| Add a Windows computer to a network | The web app gives you a one-time command: `.\wardline.exe agent install --control-url ... --token ...` |
| Run it on a Linux server (a cloud server works well) | `wardline-linux-install.tar.gz`, then `sudo sh install-linux.sh allinone https://your-address` |
| Add a Linux computer | The web app gives you the command: `sudo sh install-linux.sh agent <address> <code>` |

Windows may say "Windows protected your PC" because the program is not code-signed yet. Choose **More info**,
then **Run anyway**. Checksums are in `SHA256SUMS.txt`.

Step by step: [docs/INSTALL.md](docs/INSTALL.md). Putting the server in the cloud (about $5 a month on AWS
Lightsail), for connections that can't be reached from outside such as Starlink: [docs/AWS-LIGHTSAIL.md](docs/AWS-LIGHTSAIL.md).

## How it works

Devices connect to a server (hub and spoke). They do not connect to each other directly, and there is no
mesh or NAT traversal. The database is the source of truth: configurations are produced from it and applied
by comparing with what is running, so applying twice changes nothing. Every change is written to a
tamper-evident Activity log (append-only, hash-chained, with a verify command).

Wardline only knows about networks it created. It never imports or guesses at an existing WireGuard setup.

## Status

Early and in active use by its author. The web app, Linux and Windows computers, the cloud server, the Windows
desktop window and tray icon, and automatic server updates all work, but several Windows pieces have only
been tested on one computer. Not built yet: iPhone and Mac apps, Windows on ARM, routing to a whole home
network, code signing for Windows. See [BLOCKED.md](BLOCKED.md) for what is known to be unfinished or unchecked.

## Building and contributing

Everything builds and tests in containers; the only thing you need on your computer is Docker Desktop. No Go
toolchain is needed on the host.

```powershell
./scripts/verify.ps1      # vet, unit tests, web app tests, wording checks
./scripts/lab-up.ps1      # a lab with a control plane, two servers and simulated devices
./scripts/e2e.ps1         # end-to-end tests against the lab
./scripts/release.ps1 -Version v0.0.0   # build every download into dist/release
```

The layout and the rules the project follows are in [CLAUDE.md](CLAUDE.md); what every screen must do is in
[specs/UX.md](specs/UX.md), and the setup is in [docs/DEV-ENVIRONMENT.md](docs/DEV-ENVIRONMENT.md).

Anyone can read, copy and fork this. Only the owner and people the owner adds can push to `master` or publish
releases: everyone else is welcome to open a pull request, which the owner reviews and merges. Tests come
before code, and a test is never changed to make it pass.

## Licence

[MIT](LICENSE). The bundled Wintun driver library has its own licence, in
[internal/wintun/LICENSE.txt](internal/wintun/LICENSE.txt).
