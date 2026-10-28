# Runs the web app's Playwright tests against a fresh lab (real control plane, server and devices).
# Screenshots at 1280px and 375px wide are written to web/test-results/screens.
param([switch]$Keep)
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
$compose = @("compose", "-f", "compose.dev.yaml", "--profile", "lab", "--profile", "ui")
$down = Join-Path $PSScriptRoot "lab-down.ps1"

& powershell -ExecutionPolicy Bypass -File $down -Clean | Out-Null
docker compose -f compose.dev.yaml build dev control
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
docker compose -f compose.dev.yaml run --rm dev sh -c "cd web && pnpm install --frozen-lockfile"
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
docker @compose up -d control server-a device-linux-1 device-linux-2 agent-1
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

if (Test-Path "web/test-results") { Remove-Item -Recurse -Force "web/test-results" }
docker @compose run --rm playwright npx playwright test
$code = $LASTEXITCODE
if ($code -ne 0) {
    Write-Host "---- lab logs (last lines) ----"
    docker @compose logs --tail 30 control server-a device-linux-1 agent-1
}
if (-not $Keep) { & powershell -ExecutionPolicy Bypass -File $down -Clean | Out-Null }
exit $code
