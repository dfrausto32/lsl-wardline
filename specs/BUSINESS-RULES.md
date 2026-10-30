# Business rules

These are the rules most likely to be got wrong. Each needs a test.

## Addressing
- Users never see or type an address range. Wardline picks a private range at network creation that does not collide with common home and office ranges, and allocates device addresses automatically.
- The first usable address is the server's. Device addresses are lowest-free first.
- A removed device's address is quarantined for 7 days before reuse, so a stale config on an old phone cannot collide with a new device.
- On the server, a device's allowed addresses are its single tunnel address only. Devices cannot reach each other by default (hub-and-spoke, devices isolated by default). The network setting `devices_reach_each_other` (off by default, changed by an admin, written to Activity) makes the server relay traffic from one device to another through itself, and nothing else: relaying to or from the rest of the server's network stays blocked. Isolation is enforced by firewall rules on the server, never left to the host's own forwarding switch. Still hub-and-spoke: no mesh, no direct device-to-device connections.

## The server address
- If the user gives a **name**, all device configs use that name. Moving servers can then be done by pointing the name at the new server.
- If the user gives a raw **IP address**, the network is flagged `ip` kind. Moving to a server with a different IP requires reissuing every manual device's code. The UI warns at setup and at move time (see UX.md).

## Adding a device
1. The admin picks a person and a platform.
2. **Manual device** (phase 1, phones and computers using the official WireGuard app): Wardline generates a keypair in memory, stores only the public key, and renders the config once as a QR code and downloadable file. The private key is then discarded and is never retrievable. Mark the device `waiting`.
3. **Agent device** (phase 2+): an enrollment token is shown as a link or installer. The device generates its own keypair and sends only the public key.
4. Enrollment tokens are single use, expire in 24 hours, and only their hash is stored.
5. A device becomes `active` on its first successful connection.
6. Persistent keepalive is off for phones (battery) and on (25 s) for always-on devices.

## Status shown to the user
- `Waiting`: added, never connected.
- `Connected`: a handshake in the last 3 minutes (always-on devices) or the last 3 minutes while the device is actively used (phones and laptops).
- `Not seen recently`: otherwise, shown with "last seen" in words.
- A sleeping phone or closed laptop is normal. Wardline raises an attention banner only for always-on devices and servers, and for any device not seen in 30 days (a gentle prompt to remove it).

## Removing access
- Removal takes the device out of every server's config on the next apply, and the apply is triggered immediately. It never waits for a schedule. The device's address is quarantined.

## Refreshing security (key rotation)
- **Agent devices:** silent. The agent creates a new keypair and sends the new public key, signed with the current key. Both keys are accepted for an overlap window (default 1 hour), then the old key is retired. Default interval 90 days. A failed refresh leaves the old key working and shows an attention card to the admin. A device is never left without a working key.
- **Manual devices:** cannot refresh themselves. At 180 days a gentle card appears ("Time to refresh Sam's iPhone"). The admin issues a new code; the old key stops working only after the new device is seen connecting or after the admin confirms. Never blocks access.
- **Servers:** refreshed as part of a server move (below), or on demand.

## Expiry
Optional per-device expiry (for guests and temporary devices). At expiry the device is removed by the sweep, with an Activity entry.

## Names for machines
- Every machine has a **short name** made from its name: lowercase letters and digits, runs of anything else become one hyphen, no hyphen at either end, at most 63 characters, `machine` if nothing is left. It follows the machine's name whenever the machine is renamed, unless the person set a short name by hand (then it stays until they clear it).
- A short name is unique among the network's machines, **extra names** and the server. If taken, a number is added: `office-pc-2`. A short name typed by hand that is taken or reserved is refused with a plain message, never numbered.
- Reserved: `server`, `localhost`, `wardline`. The server is always `server`.
- Extra names follow the same character rules and uniqueness. A machine can have up to 10. Removing a machine frees all its names.
- The full name is the name plus `.wardline.internal`. The ending is fixed for now.
- **Names answer only while "Machines can reach each other" is on.** While it is off the server answers no name and machines are told to use no name service. Turning it on or off bumps the desired-state version like any other change.
- The name service runs on the server at its own network address (UDP 53). It answers only the fixed ending: A records for machines, the server and extra names; an empty answer for other record types of a known name; "no such name" for an unknown name under the ending; refused for anything else. It never forwards anything and keeps no log of lookups.
- A machine is told (in its desired state) the ending and the server's address, and sets its own system to ask the server for that ending only. Everything else keeps using the machine's normal name service. If the machine's system cannot do this, the machine reports it and the device page says so; nothing else breaks.
- Machines that already exist get short names from their current names the first time this version runs; nothing is renamed afterwards unless the person renames it.

## Moving to a new server
State machine: `planned` then `preflight` then `provisioned` then `testing` then `switching` then `verifying` then `soaking` then `finished`. Any state before `finished` can go to `rolled_back` with one action.
- **Strategy is chosen automatically, never by the user.**
  - `new_key`: used when every active device is an agent device. Agents hold both servers during the switch, so no secret moves.
  - `carry_key`: used when any manual device exists. The new server needs the old server's key so existing codes keep working. The old server seals its private key to a one-time public key generated by the new server and relays it through the control plane; the control plane cannot read it. Then the server address (a name) is pointed at the new server. If the address is a raw IP, the move first requires reissuing codes (UX flow 7).
- `preflight` must pass before anything changes: the new server is reachable, the port is open, forwarding works, and the clock is sane.
- `testing`: a test device or a server-side loopback check proves the new server accepts connections before any real device moves.
- `verifying` passes when every device that was `Connected` before the switch has connected to the new server. Devices that were asleep are listed as "will switch when they next connect", not as failures.
- `soaking`: the old server stays up for a configurable time (default 24 hours). "Go back to the old server" is one action throughout.
- `finished` requires an explicit user action. Never automatic.
- Every transition writes an Activity entry. Finishing writes a report the user can download.
- The control plane is not moved by this flow. Control plane backup and restore is a separate feature (phase 4).

## Activity (audit log)
- Append-only, hash-chained. Each row stores the plain sentence shown to users. A verify command recomputes the chain. Exportable as CSV.

## Sign-in
- Admin password hashed with argon2id. Optional authenticator-app code (TOTP). Sessions expire. Rate limit sign-in attempts. No default credentials ever.
