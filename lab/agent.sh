#!/bin/sh
# Lab computer with the Wardline agent. It waits for its setup code in /lab, runs the real agent,
# and pings the server's private address once a second, recording the time of the last success.
# Removing the device makes the agent delete its tunnel and keys, so the pings stop.
dev="$1"
while [ ! -s "/lab/$dev.token" ]; do sleep 1; done
export WARDLINE_ENROLL_TOKEN="$(cat "/lab/$dev.token")"
export WARDLINE_STATE_DIR="/lab/state-$dev"
wardline agent --log-level debug --refresh-check 2s &
while [ ! -s "/lab/$dev.target" ]; do sleep 1; done
target="$(cat "/lab/$dev.target")"
echo "$dev: agent started, checking $target"
while true; do
  if ping -c1 -W1 "$target" >/dev/null 2>&1; then
    date +%s > "/lab/$dev.lastok.tmp" && mv "/lab/$dev.lastok.tmp" "/lab/$dev.lastok"
  fi
  sleep 1
done
