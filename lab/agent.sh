#!/bin/sh
# Lab computer with the Wardline agent. It waits for its setup code in /lab, runs the real agent,
# and pings the server's private address once a second, recording the time of the last success.
# If /lab/<name>.peer holds another computer's private address, it pings that one too and records
# the last success in <name>.peerok (used to test whether devices can reach each other).
# If /lab/<name>.lookup holds "<name> <server-address>", it asks the server's name service for that name
# once a second and records the answer (or the error) in <name>.lookupout.
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
  if [ -s "/lab/$dev.peer" ]; then
    if ping -c1 -W1 "$(cat "/lab/$dev.peer")" >/dev/null 2>&1; then
      date +%s > "/lab/$dev.peerok.tmp" && mv "/lab/$dev.peerok.tmp" "/lab/$dev.peerok"
    fi
  fi
  if [ -s "/lab/$dev.lookup" ]; then
    wardline names lookup $(cat "/lab/$dev.lookup") > "/lab/$dev.lookupout.tmp" 2>&1
    mv "/lab/$dev.lookupout.tmp" "/lab/$dev.lookupout"
  fi
  sleep 1
done
