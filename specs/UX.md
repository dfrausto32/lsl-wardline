# UX: built for people who do not know networking

## Who it is for
Someone who can install an app and scan a QR code but has never heard of a subnet. Examples: a small-business owner securing remote access for staff, a family member getting a phone onto the home network. If they have to search the web to understand a screen, the screen is wrong.

## Principles
1. **One obvious next action per screen.** A single primary button. Everything else is secondary.
2. **Say what happened and what to do, in plain words.** Never show a raw error. Format: what happened, why it matters, the button that fixes it.
3. **Show progress live.** "Waiting for your iPhone to connect..." then a green check the moment it does. No refresh button.
4. **Confirm before anything destructive, and say what it will do.** "Remove access for Sam's laptop? It will be disconnected right away. You can add it again later."
5. **Defaults are safe and automatic.** Rotation, addressing, and settings are invisible unless the user opens Advanced.
6. **Technical detail lives behind an Advanced drawer**, collapsed by default, never required.
7. **Mobile first.** Every flow works one-handed on an iPhone-sized screen (375px wide) and installs as a home-screen app.
8. **Accessible.** Keyboard navigable, labelled controls, readable contrast, no information by colour alone.

## Vocabulary (primary UI must use the left column)
| Say | Never say (outside Advanced) |
|---|---|
| Device | peer, node, client |
| Server | gateway, hub, endpoint |
| Server address | endpoint, host:port |
| Private connection / network | VPN, tunnel, interface |
| Connected / Not seen recently / Waiting | handshake, stale |
| Remove access | revoke |
| Refresh security | rotate keys |
| Move to a new server | migrate |
| Activity | audit log |
| Security code / QR code | public key, private key, config |
| Person | user, account (for the people who own devices) |

A test scans the UI string file and fails if any "never say" term appears outside the Advanced namespace.

## Core flows

### 1. First-run setup (one screen at a time)
1. "Welcome to Wardline. Let's set up your private network." Name it (default suggestion).
2. Create the admin sign-in (email + password, then optional authenticator app).
3. "Where will your server live?" Choices: *On this computer / on a server I run* (shows one copy-paste command or a compose file) and *I'll do this later*.
4. "What address will devices use to reach it?" Explains in one sentence, accepts a name like `network.example.com` or an IP address. If an IP is given, show a clear note: "If you change servers later, devices will need new codes. A name avoids that." Recommend a name.
5. Live check: "Waiting for your server to check in..." then green check.
6. Land on Home with a single card: "Add your first device".

### 2. Add a device
1. "Add a device" button on Home (always visible, including a floating button on mobile).
2. "Whose device is it?" Pick or create a person (just a name).
3. "What kind of device?" Big buttons: iPhone, Android, Windows, Mac, Linux.
4. Show the right instructions for that device, minimal:
   - iPhone/Android: "Install the WireGuard app" (store link), then "Scan this code" with a QR code on screen. Also a "Send me a link" option for a person's own phone.
   - Windows/Mac/Linux (phase 1): "Download this file" and "Open it in the WireGuard app", with the install link. Phase 2 replaces this with a one-click installer.
5. Live status: "Waiting for Sam's iPhone to connect..." then "Connected" with a green check and "Done".
6. The code or file is shown once. Say so plainly: "This code works once. If it's lost, remove the device and add it again."

### 3. Home
- A list of people and their devices as cards. Each card: name, device type icon, status chip (Connected / Not seen recently / Waiting), last seen in words ("2 minutes ago").
- A banner only when something needs attention, with one button. Phones that are simply asleep are never an alarm.
- Footer: server status ("Your server is online").

### 4. Device details
Status, last seen, when it was added, "Refresh security" (if due), "Remove access". Advanced drawer: public key, address, transfer stats.

### 5. Remove access
Confirm dialog (principle 4). Takes effect within seconds. Appears in Activity.

### 6. Refresh security
Normally invisible. For devices that cannot refresh themselves (phones in phase 1), a gentle card: "Time to refresh Sam's iPhone. It takes a minute: scan a new code." Never blocks anything, never red.

### 7. Move to a new server
Wizard, one step per screen:
1. "Why move?" is not asked. Start with "Set up the new server": show the one command or file for the new machine. Live "Waiting for the new server..."
2. "Test the new server": runs a safe check with a test device if available, shows green checks in words ("The new server is reachable", "Devices can connect").
3. "Move everyone": explains what will happen and how long it takes. One button.
4. Live progress: "12 of 14 devices are using the new server." Devices that are asleep are listed as "will switch when they next connect", not as failures.
5. "Keep the old server for 24 hours in case anything goes wrong?" Default yes. A visible "Go back to the old server" button during this time.
6. "Finish and shut down the old server" only when the user chooses to.
If the server address is a raw IP address, step 1 stops and explains that devices will need new codes, and offers to do it as part of the move.

### 8. Activity
A plain-language timeline: "Maria added Sam's iPhone", "Sam's laptop was removed", "Server moved". Filter by person or device. Export as CSV.

## Message style
- Short sentences. Plain verbs. No exclamation marks, no jokes in errors.
- Never blame the user. Never show stack traces, status codes, or key material.
- Times are relative ("2 minutes ago") with the exact time on hover.

## Empty states
Every empty screen shows what the screen is for and the one button that fills it.
