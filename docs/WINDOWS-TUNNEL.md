# Windows tunnel and installer hosting

## Decision 1 (changed 2026-10-29): one program, nothing else to install

Dylan wants one install for the user. The Windows agent therefore carries its own WireGuard and does not need the official WireGuard for Windows app.

- **Engine:** the same userspace WireGuard (`wireguard-go`) the Linux server falls back to, running inside the `WardlineAgent` service. The tunnel lives and dies with the service.
- **Driver:** WireGuard's signed Wintun driver library, `wintun.dll` (version 0.14.1, amd64), embedded in `wardline.exe` (`internal/wintun`). `agent install` writes it next to the installed program in `C:\Program Files\Wardline`; the tunnel code loads it from there. The service runs as the system account, which may create the adapter.
- **Address and MTU:** set with `netsh` (`internal/wg/link_windows.go`); the commands are built in `internal/wg/netsh.go` and unit-tested on any system.
- **Keys:** generated on the computer, kept in `C:\ProgramData\Wardline`, readable only by the system and administrators. Never on a command line, never sent anywhere.
- **Why this over driving the official app:** one file to download, no second installer, no dependence on another program's version or command line. The cost is that Wardline now ships the Wintun library and must release a new version when Wintun does.

### Licence of wintun.dll
The Prebuilt Binaries License (`internal/wintun/LICENSE.txt`, shipped with every release) allows redistribution "alongside other software that uses the Software only via the Permitted API" and forbids modifying it. Wardline embeds the file byte for byte, unchanged, and uses it only through wireguard-go's wrapper around the published `wintun.h` interface. A test pins the file's SHA-256 so it cannot change by accident. The licence is not legal advice; it is Dylan's call to accept it. Recorded in `BLOCKED.md`.

### Replaces
Decision 1 until 2026-10-28 was to drive the official WireGuard for Windows (`wireguard /installtunnelservice` and `wg syncconf`), with the installer only checking that it was present. That code (`internal/wg/wgapp.go`) was removed. The checksum mismatch found in WireGuard's own installer manifest no longer matters because nothing is downloaded.

### Not covered
- Windows on ARM (the release builds amd64 only).
- Windows keys use a locked folder, not DPAPI.
- Nothing here has run on a real Windows machine (manual check M6).

## Decision 2: installers published on GitHub Releases

Windows and Linux installers are built by `scripts/release.ps1` and attached to a GitHub Release at https://github.com/dfrausto32/lsl-wardline/releases. The web app links to the latest release (`releases/latest/download/...`). The Windows file is named `wardline.exe` so the command the web app shows runs as is. The licence file for Wintun is attached to every release.
