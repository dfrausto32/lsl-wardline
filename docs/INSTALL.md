# Installing Wardline

Wardline has two parts that can run on the same machine or on different ones:

- **The control plane** is the web app you sign in to. Run it somewhere small and stable. A separate host is best, because moving to a new server never touches it.
- **The server** is the machine your devices connect to. Wardline gives you one copy-and-paste command for it inside the web app.

Everything is one program, `wardline`. On a Linux machine the best way to run it is as a **systemd service**: no Docker, nothing to publish, it starts at boot and restarts if it stops. Containers are covered at the end for people who prefer them.

## 1. Get the program onto the machine

Build everything from the `app` folder on any machine with Docker. This also builds the web app into the program:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/release.ps1
```

It writes `dist/release/`:

- `wardline-linux-install.tar.gz`: the Linux program with its service files and install script. This is what a Linux machine needs.
- `wardline-linux-amd64`, `wardline-linux-arm64`, `wardline.exe` (Windows): the bare programs.
- `SHA256SUMS.txt`: checksums, so a copy can be checked.

Copy the tarball to the machine (for example with `scp`) and unpack it into a folder:

```bash
scp dist/release/wardline-linux-install.tar.gz you@your-server:~/
ssh you@your-server "mkdir -p wardline && tar -xzf wardline-linux-install.tar.gz -C wardline"
```

Publishing these as a GitHub Release is one command once a repository exists; `release.ps1` prints it.

## 2. Start the control plane

On the machine that will host the web app:

```bash
cd ~/wardline
sudo sh install-linux.sh control https://wardline.example.com
```

- The address is where your servers will reach the control plane. Put the control plane behind an HTTPS reverse proxy (Caddy, nginx, or your host's load balancer) that forwards to port 8080, and use that address here.
- It stores its database and the secret key that protects stored secrets in `/var/lib/wardline-control`. **Back up that whole folder.** Without the key the stored secrets cannot be read.

Open the address in a browser. Wardline walks you through the rest one screen at a time.

## 3. Add your server

In the web app, choose where your server will live. On the "Linux service" tab Wardline shows one command with your own one-time code filled in:

```bash
sudo sh install-linux.sh gateway https://wardline.example.com <one-time code>
```

Run it on the machine that devices will connect to, from the folder that holds the program and the `deploy/` files. The code works once and expires after 24 hours. The page updates by itself when the server checks in.

The server needs UDP port 51820 reachable from the internet. If your host or router blocks it, open that port.

Your devices connect using an address you choose in the setup. If you own a domain, add a name such as `network.example.com` with an A record pointing at this machine's public IP. Moving to a new server later then means changing that one record.

### Using a Windows computer as the server

Everything is one file, `wardline.exe`, and nothing else needs installing. Download it from the [latest release](https://github.com/dfrausto32/lsl-wardline/releases/latest).

**The easy way: double-click it.** Windows asks for permission, then a Wardline window opens (no black command window). Choose **Make this computer my server**. It installs the web app, which also registers this computer as your server, and then the same window shows the Wardline setup: create your sign-in, name your network and choose the address devices use. There is no command to copy.

Wardline opens in your normal web browser when the install finishes, and a **Wardline** icon is added to the desktop that opens it again. The icon is removed with Remove Wardline.

**The server address.** You can use this computer's own address (for example `192.168.1.50`); the setup screen fills it in for you. It works for devices on the same network. Two things to know: a computer's address can change, so give this one a fixed address in your router (often called "DHCP reservation" or "static lease"); and for devices away from home press **Find my public address** on that screen (it asks api.ipify.org, a free service that answers with the address you ask from) and add a router rule forwarding UDP port 51820 to this computer. Wardline cannot test that rule for you; the first device that connects from outside proves it. A name such as `network.example.com` is the better long-term choice because you can repoint it later without touching any device.

**"Windows protected your PC".** Windows shows this for any program it has not seen many times that is not signed with a paid certificate. Choose **More info**, then **Run anyway**. It goes away once Wardline is signed (see `BLOCKED.md`). Do not turn Windows protection off.

**With commands instead:** open PowerShell **as administrator** in the folder that holds `wardline.exe`.

```powershell
.\wardline.exe control install --with-server
```

This installs the web app and the server in one service, opens its ports (the web port only for home and work networks), and prints the address other computers use. Add `--public-url` to use a different one. Then open `http://localhost:8080` and follow the setup screens; the server registers itself.

If the server runs on a different computer than the web app, leave out `--with-server`, and on the server computer run the command from the setup page's **Windows service** tab (`.\wardline.exe gateway install --control-url ... --token ...`).

The program lives in `C:\Program Files\Wardline`. Data is in `C:\ProgramData\Wardline-control` (database and secret key: **back it up**) and `C:\ProgramData\Wardline-server`, readable only by the system and administrators. Both start with Windows and restart if they stop.

To take them off again, use **Remove Wardline from this computer** in the window, or `.\wardline.exe control uninstall` and `.\wardline.exe gateway uninstall`. Data folders are kept.

In PowerShell, `wardline.exe` is a Windows app, so a command's output may appear after the prompt has already come back. That is normal.

## 4. Add computers

In the web app choose **Add a device**, pick whose it is, then **Windows** or **Linux**. Wardline shows one command with a one-time code. The computer makes its own security key and sends only the public half, so Wardline never sees the secret one. It then keeps itself connected, replaces its key every 90 days without anyone doing anything, follows the server if its address changes, and deletes its connection and keys by itself if you remove it in Wardline.

**Linux:** unpack the same tarball on the computer and run the command Wardline shows:

```bash
sudo sh install-linux.sh agent https://wardline.example.com <one-time code>
```

**Windows:** nothing else needs installing; `wardline.exe` carries its own connection software. Download it (the Add a device page links to it), open PowerShell **as administrator** in that folder and run the command Wardline shows:

```powershell
.\wardline.exe agent install --control-url https://wardline.example.com --token <one-time code>
```

It copies the program and its network driver file (`wintun.dll`) to `C:\Program Files\Wardline`, keeps its keys in `C:\ProgramData\Wardline` (readable only by the system and administrators), creates a "Wardline" service that starts with Windows and restarts if it stops, and starts it. `wardline.exe agent uninstall` takes it off the computer again.

Phones and Macs still use the official WireGuard app with a code or file.

To make a computer refresh its security right now instead of waiting (for example to test it): `wardline agent refresh` on that computer.

## Running and checking the services

```bash
systemctl status wardline-control      # or wardline-gateway
journalctl -u wardline-gateway -f      # live messages
sudo systemctl restart wardline-gateway
```

To update, copy the new `wardline` over and run the same install command again; it replaces the program and restarts the service. The server keeps its identity in `/var/lib/wardline-gateway`, so it does not need a new code.

What the install does: creates a `wardline` system user, copies the program to `/usr/local/bin`, writes `/etc/wardline/<role>.env` (readable only by root and the service), installs the service file and starts it. The server service is allowed only to create its network interface (`CAP_NET_ADMIN`); the control plane runs with no extra privileges.

On Linux the server uses the kernel's WireGuard when it is available and a built-in userspace version when it is not.

## Checking the Activity log

```bash
sudo -u wardline wardline audit verify --data /var/lib/wardline-control
sudo -u wardline wardline audit export --data /var/lib/wardline-control > activity.csv
```

`audit verify` tells you if any entry was changed or removed.

## Using containers instead

If you would rather use Docker, build the image once from the `app` folder:

```bash
docker build -t lsl-wardline:latest .
```

Control plane:

```bash
docker run -d --name wardline-control --restart unless-stopped \
  -p 8080:8080 \
  -e WARDLINE_PUBLIC_URL=https://wardline.example.com \
  -v wardline-control:/var/lib/wardline \
  lsl-wardline:latest control
```

Server (the web app also shows this, with the code filled in, on its "Container command" and "Compose file" tabs):

```bash
docker run -d --name wardline-server --restart unless-stopped \
  --cap-add NET_ADMIN --device /dev/net/tun -p 51820:51820/udp \
  -e WARDLINE_CONTROL_URL=https://wardline.example.com \
  -e WARDLINE_ENROLL_TOKEN=<one-time code> \
  -v wardline-server:/var/lib/wardline \
  lsl-wardline:latest gateway
```

The image is built locally; there is no public registry yet.

## Developing and testing

Everything runs in containers; the only requirement is Docker Desktop. See `docs/DEV-ENVIRONMENT.md`.
