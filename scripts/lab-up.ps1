# Starts the lab: control plane, two servers and two simulated devices on a private network.
# The control plane is then at http://localhost:8080
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
docker compose -f compose.dev.yaml build dev control
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
docker compose -f compose.dev.yaml --profile lab up -d control server-a server-b device-linux-1 device-linux-2
exit $LASTEXITCODE
