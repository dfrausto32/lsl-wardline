# Stops the lab. Add -Clean to also remove the lab's containers and its own wardline_ volumes
# (not the Go and pnpm caches, which only make rebuilds faster).
param([switch]$Clean)
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
docker compose -f compose.dev.yaml --profile lab down --remove-orphans
$code = $LASTEXITCODE
if ($Clean) {
    foreach ($v in "wardline_control-data", "wardline_server-a-state", "wardline_server-b-state", "wardline_lab-shared") {
        docker volume rm $v 2>$null | Out-Null
    }
}
exit $code
