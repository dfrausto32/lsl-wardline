#!/bin/sh
# Lab server: waits for the install code that the end-to-end test drops in /lab, then runs the server role.
name="$1"
while [ ! -s "/lab/token-$name" ]; do sleep 1; done
export WARDLINE_ENROLL_TOKEN="$(cat "/lab/token-$name")"
exec wardline gateway --name "$name" --log-level debug
