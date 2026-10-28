# Development environment: Docker Desktop only

Goal: develop, test, and run a realistic lab without WSL distros or a host Go install. The only host requirement is Docker Desktop (Linux containers).

## Files Claude Code creates in task 1.0
- `Dockerfile.dev`: Go toolchain, `iproute2`, `wireguard-tools`, `golangci-lint`, Node + pnpm for the UI. Used by the `dev` service.
- `compose.dev.yaml`: services
  - `dev`: runs builds and tests. Repo bind-mounted at `/src`; Go module and build caches in named volumes (fast rebuilds, easy to delete).
  - `playwright`: official Playwright image for UI tests.
  - `lab` profile: `control`, `server-a`, `server-b`, and two or three simulated devices (`device-linux-1`, `device-linux-2`) on a private bridge network. Servers and devices need `cap_add: NET_ADMIN` and `/dev/net/tun`. This lab is how server moves and rotation are rehearsed without touching real machines.
- `Dockerfile`: the production image (control plane + server in one binary).
- `scripts/*.ps1`: thin wrappers around `docker compose` so Dylan runs one command.

## WireGuard inside Docker Desktop
Containers share the Docker Desktop VM's Linux kernel. Whether it includes the WireGuard kernel module is checked in task 1.0, not assumed:

```
docker run --rm --cap-add NET_ADMIN alpine sh -c "apk add -q iproute2 && ip link add wgtest type wireguard && echo kernel-wireguard-ok"
```

- If it works, the kernel backend runs in the lab and contract tests include it.
- If it fails, the userspace backend (`wireguard-go`, needs `/dev/net/tun`) carries the lab and the tests. Nothing else changes. Record the result in `BLOCKED.md` as information, not a blocker.

## Disk and cleanup
- Everything Wardline creates is in containers, images, and named volumes with the prefix `wardline_`. `./scripts/lab-down.ps1 -Clean` removes the lab's containers and volumes.
- Never run `docker system prune` or delete volumes outside the `wardline_` prefix. That is Dylan's call.

## What containers cannot test (manual checks)
Windows agent and Windows behaviour, iPhone app, and real-world NAT and mobile networks. These are tracked in `specs/ACCEPTANCE.md` as manual checks.
