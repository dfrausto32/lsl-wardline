# Installing Wardline

Wardline has two parts that can run on the same machine or on different ones:

- **The control plane** is the web app you sign in to. Run it somewhere small and stable. A separate host is best, because moving to a new server never touches it.
- **The server** is the machine your devices connect to. Wardline gives you one copy-and-paste command for it inside the web app.

Everything is one program, `wardline`. On a Linux machine the best way to run it is as a **systemd service**: no Docker, nothing to publish, it starts at boot and restarts if it stops. Containers are covered at the end for people who prefer them.

## 1. Get the program onto the machine

Build it from the `app` folder on any machine with Docker. This also builds the web app into it:

```bash
docker build --platform linux/amd64 --target binary --output dist .
```

Use `--platform linux/arm64` for an ARM server. The program is `dist/wardline`, one static file.

Copy `dist/wardline` and the `deploy/` folder to the machine (for example with `scp`) and put them in one folder:

```bash
scp dist/wardline deploy/* you@your-server:~/wardline/
```

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
