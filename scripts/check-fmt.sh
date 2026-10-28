#!/bin/sh
# Fails if any Go file is not gofmt-clean.
out=$(gofmt -l cmd internal e2e 2>/dev/null)
if [ -n "$out" ]; then
  echo "These files need gofmt:"
  echo "$out"
  exit 1
fi
