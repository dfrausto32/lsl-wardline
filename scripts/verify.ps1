# Runs everything that must be green before a task is done, inside containers.
# Usage: powershell -ExecutionPolicy Bypass -File scripts/verify.ps1
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")

function Step($name, [scriptblock]$cmd) {
    Write-Host "==> $name"
    & $cmd
    if ($LASTEXITCODE -ne 0) { Write-Host "FAILED: $name"; exit $LASTEXITCODE }
}

Step "build dev image" { docker compose -f compose.dev.yaml build dev }
Step "gofmt" { docker compose -f compose.dev.yaml run --rm dev sh scripts/check-fmt.sh }
Step "go vet" { docker compose -f compose.dev.yaml run --rm dev go vet ./... }
Step "go test" { docker compose -f compose.dev.yaml run --rm dev go test ./... }

if (Test-Path "web/package.json") {
    Step "web: install, typecheck, unit tests (strings lint)" {
        docker compose -f compose.dev.yaml run --rm dev sh -c "cd web && pnpm install --frozen-lockfile && pnpm run typecheck && pnpm run test:unit"
    }
    Step "web: build" { docker compose -f compose.dev.yaml run --rm dev sh -c "cd web && pnpm run build" }
    Step "web: playwright" {
        docker compose -f compose.dev.yaml --profile ui run --rm playwright sh -c "npx playwright test"
    }
}
Write-Host "verify: all steps passed"
exit 0
