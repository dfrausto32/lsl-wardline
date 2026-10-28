# Installing Wardline

Wardline has two parts that can run on the same machine or on different ones:

- **The control plane** is the web app you sign in to. Run it somewhere small and stable. A separate host is best, because moving to a new server never touches it.
- **The server** is the machine your devices connect to. Wardline gives you one copy-and-paste command for it inside the web app.

Everything runs as one program, `wardline`. The Docker image is the easiest way to run it.

## 1. Build the image (until a public registry exists)

From the `app` folder on any machine with Docker:

```bash
docker build -t lsl-wardline:latest .
```

## 2. Start the control plane

```bash
docker run -d --name wardline-control --restart unless-stopped \
  -p 8080:8080 \
  -e WARDLINE_PUBLIC_URL=https://wardline.example.com \
  -v wardline-control:/var/lib/wardline \
  lsl-wardline:latest control
```

- `WARDLINE_PUBLIC_URL` is the address your servers use to reach the control plane. Put the control plane behind an HTTPS reverse proxy (Caddy, nginx, or your host's load balancer) and use that address here.
- The volume holds the database and the secret key that protects stored secrets. **Back both up together.** Without the key the stored secrets cannot be read.

Open the address in a browser. Wardline walks you through the rest one screen at a time.

## 3. Add your server

In the web app, choose where your server will live. Wardline shows one command like this, with your own one-time code filled in:

```bash
docker run -d --name wardline-server --restart unless-stopped \
  --cap-add NET_ADMIN --device /dev/net/tun -p 51820:51820/udp \
  -e WARDLINE_CONTROL_URL=https://wardline.example.com \
  -e WARDLINE_ENROLL_TOKEN=<one-time code> \
  -v wardline-server:/var/lib/wardline \
  lsl-wardline:latest gateway
```

Run it on the machine that devices will connect to. The code works once and expires after 24 hours. The page updates by itself when the server checks in.

The server needs UDP port 51820 reachable from the internet. If your host or router blocks it, open that port.

## Without Docker

Download or build the `wardline` binary for your system and run the same commands without the `docker run` part:

```bash
wardline control --data /var/lib/wardline --public-url https://wardline.example.com
wardline gateway --control-url https://wardline.example.com --token <one-time code>
```

On Linux the server uses the kernel's WireGuard when it is available and a built-in userspace version when it is not.

## Checking the Activity log

```bash
docker exec wardline-control wardline audit verify
docker exec wardline-control wardline audit export > activity.csv
```

`audit verify` tells you if any entry was changed or removed.

## Developing and testing

Everything runs in containers; the only requirement is Docker Desktop. See `docs/DEV-ENVIRONMENT.md`.
