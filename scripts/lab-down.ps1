# Stops the lab. Add -Clean to also remove the lab's containers and its own wardline_ volumes
# (not the Go and pnpm caches, which only make rebuilds faster).
param([switch]$Clean)
$ErrorActionPreference = "Continue"
Set-Location (Join-Path $PSScriptRoot "..")
docker compose -f compose.dev.yaml --profile lab --profile ui down --remove-orphans 2>&1 | Out-Null
$code = $LASTEXITCODE
if ($Clean) {
    foreach ($v in "wardline_control-data", "wardline_server-a-state", "wardline_server-b-state", "wardline_lab-shared") {
        # -f makes a volume that was never created a non-error
        docker volume rm -f $v 2>&1 | Out-Null
    }
}
exit $code
