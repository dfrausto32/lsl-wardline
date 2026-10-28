#!/bin/sh
# Simulated device: waits for its config in /lab, brings the tunnel up with the
# official wg-quick tool, then pings the server's private address once a second
# and records the time of the last success. Removing access makes that stop.
dev="$1"
while [ ! -s "/lab/$dev.conf" ] || [ ! -s "/lab/$dev.target" ]; do sleep 1; done
mkdir -p /etc/wireguard
cp "/lab/$dev.conf" /etc/wireguard/wg0.conf
chmod 600 /etc/wireguard/wg0.conf
wg-quick up wg0 || exit 1
target="$(cat "/lab/$dev.target")"
echo "$dev: tunnel up, checking $target"
while true; do
  if ping -c1 -W1 "$target" >/dev/null 2>&1; then
    date +%s > "/lab/$dev.lastok.tmp" && mv "/lab/$dev.lastok.tmp" "/lab/$dev.lastok"
  fi
  sleep 1
done
