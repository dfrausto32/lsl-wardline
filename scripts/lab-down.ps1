# Stops the lab. Add -Clean to also remove the lab's containers and wardline_ volumes.
param([switch]$Clean)
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
if ($Clean) {
    docker compose -f compose.dev.yaml --profile lab down --volumes --remove-orphans
} else {
    docker compose -f compose.dev.yaml --profile lab down --remove-orphans
}
exit $LASTEXITCODE
