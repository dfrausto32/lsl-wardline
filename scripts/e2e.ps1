# End-to-end tests against a fresh lab. Add -Keep to leave the lab running afterwards.
param([switch]$Keep)
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
$compose = @("compose", "-f", "compose.dev.yaml", "--profile", "lab")
$down = Join-Path $PSScriptRoot "lab-down.ps1"

# Start from nothing so the run is repeatable. Removes only this lab's containers and wardline_ lab volumes.
& powershell -ExecutionPolicy Bypass -File $down -Clean | Out-Null
docker compose -f compose.dev.yaml build dev control
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
docker @compose up -d control server-a server-b device-linux-1 device-linux-2
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

docker @compose run --rm e2e
$code = $LASTEXITCODE
if ($code -ne 0) {
    Write-Host "---- lab logs (last lines) ----"
    docker @compose logs --tail 40 control server-a device-linux-1 device-linux-2
}
if (-not $Keep) { & powershell -ExecutionPolicy Bypass -File $down -Clean | Out-Null }
exit $code
