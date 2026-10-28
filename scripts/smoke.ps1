# Checks the container dev environment and reports whether kernel WireGuard exists.
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

docker version --format '{{.Server.Version}}' | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Host "Docker Desktop is not running."; exit 1 }
docker compose version | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Host "docker compose is not available."; exit 1 }

docker compose -f compose.dev.yaml build dev
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
docker compose -f compose.dev.yaml run --rm dev sh -c "go version && node --version && pnpm --version && golangci-lint --version && wg --version && ip -V"
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$out = docker compose -f compose.dev.yaml run --rm dev go test -v -count=1 ./internal/wg/ -run TestEnv
$code = $LASTEXITCODE
$out | ForEach-Object { Write-Host $_ }
if ($code -ne 0) { exit $code }
$line = $out | Where-Object { $_ -match "KERNEL-WIREGUARD" } | Select-Object -First 1
Write-Host ""
Write-Host "Smoke test passed. $($line.Trim())"
exit 0
