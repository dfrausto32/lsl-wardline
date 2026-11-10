# web/node_modules on this computer must stay empty. The real modules live in the "webmodules" Docker volume, which the
# dev and playwright containers mount over this folder. If an install ever wrote into the Windows folder, it holds Linux
# links that Windows cannot open, and Obsidian (and Explorer) fail on them. This empties the folder and says so.
# Usage: called by verify.ps1 and ui-test.ps1. Safe to run any time.
$ErrorActionPreference = "Stop"
$nm = Join-Path $PSScriptRoot "..\web\node_modules"
if (-not (Test-Path $nm)) { exit 0 }
$left = @(Get-ChildItem $nm -Force)
if ($left.Count -eq 0) { exit 0 }
Write-Host "web/node_modules on this computer was not empty ($($left.Count) entries). Emptying it: the containers keep their own copy in a Docker volume."
foreach ($item in $left) {
    if ($item.PSIsContainer) { cmd /c "rmdir /s /q `"$($item.FullName)`"" 2>&1 | Out-Null }
    else { Remove-Item $item.FullName -Force }
}
if (@(Get-ChildItem $nm -Force).Count -gt 0) {
    Write-Host "Could not empty web/node_modules. Close anything using it and run this script again."
    exit 1
}
Write-Host "web/node_modules is empty again. Do not run pnpm or npm install on this computer; run it through docker compose."
exit 0
