# Checks that the single binary cross-compiles for the platforms we ship, inside the dev container.
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
docker compose -f compose.dev.yaml run --rm dev sh scripts/crosscompile.sh
exit $LASTEXITCODE
