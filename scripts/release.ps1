# Builds everything someone needs to install Wardline, into dist/release:
#   wardline-linux-amd64, wardline-linux-arm64, wardline.exe (Windows)
#   wardline-linux-install.tar.gz   the program for Linux plus the service files and install script
#   SHA256SUMS.txt                  checksums of every file above
# Nothing is published. Publishing to GitHub Releases needs a repository first (see BLOCKED.md); the command is printed at the end.
param([string]$Version = "")
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
if (-not $Version) { $Version = (git describe --tags --always --dirty 2>$null); if (-not $Version) { $Version = "dev" } }

$out = "dist/release"
if (Test-Path $out) { Remove-Item -Recurse -Force $out }
New-Item -ItemType Directory -Force $out | Out-Null

$targets = @(
    @{ os = "linux"; arch = "amd64"; ext = "" },
    @{ os = "linux"; arch = "arm64"; ext = "" },
    @{ os = "windows"; arch = "amd64"; ext = ".exe" }
)
foreach ($t in $targets) {
    $tmp = "dist/build-$($t.os)-$($t.arch)"
    docker build --target binary --build-arg "GOOS=$($t.os)" --build-arg "GOARCH=$($t.arch)" --build-arg "VERSION=$Version" --output $tmp .
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    # The Windows program is plain wardline.exe so the command the web app shows runs as is.
    $name = if ($t.os -eq "windows") { "wardline.exe" } else { "wardline-$($t.os)-$($t.arch)" }
    Move-Item "$tmp/wardline" "$out/$name"
    Remove-Item -Recurse -Force $tmp
}

# Linux bundle: the program next to the files install-linux.sh expects.
$bundle = "dist/bundle"
if (Test-Path $bundle) { Remove-Item -Recurse -Force $bundle }
New-Item -ItemType Directory -Force $bundle | Out-Null
Copy-Item "$out/wardline-linux-amd64" "$bundle/wardline"
Copy-Item deploy/* $bundle
docker run --rm -v "${PWD}/dist:/d" -w /d debian:bookworm-slim sh -c "chmod +x bundle/wardline && tar -czf release/wardline-linux-install.tar.gz -C bundle ."
Remove-Item -Recurse -Force $bundle

# wardline.exe carries the Wintun driver library; its licence goes with every release.
Copy-Item internal/wintun/LICENSE.txt "$out/wintun-LICENSE.txt"

# Linux checks this file with sha256sum, so it must end its lines with LF, not Windows' CRLF.
$sums = Get-ChildItem $out -File | Where-Object { $_.Name -ne "SHA256SUMS.txt" } | ForEach-Object {
    "{0}  {1}" -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower(), $_.Name
}
[System.IO.File]::WriteAllText((Join-Path (Resolve-Path $out) "SHA256SUMS.txt"), (($sums -join "`n") + "`n"), [System.Text.Encoding]::ASCII)

# Servers that update themselves install a release only if SHA256SUMS.txt is signed with the owner's key.
# The private key lives on this computer only (see docs/AUTO-UPDATE.md), never in the repository.
$keyDir = if ($env:WARDLINE_SIGNING_DIR) { $env:WARDLINE_SIGNING_DIR } else { Join-Path $env:USERPROFILE ".wardline-signing" }
if (Test-Path (Join-Path $keyDir "release.key")) {
    docker compose -f compose.dev.yaml run --rm -v "${keyDir}:/keys:ro" dev go run ./tools/sign sign /keys/release.key dist/release/SHA256SUMS.txt
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    Write-Host "Signed SHA256SUMS.txt (SHA256SUMS.txt.sig). Publish this file with the others."
} else {
    Write-Warning "No signing key at $keyDir. This release is NOT signed, so servers that update themselves will ignore it."
}

Get-ChildItem $out | Format-Table Name, Length
Write-Host "Version: $Version"
Write-Host "To publish once the GitHub repository exists:"
Write-Host "  gh release create $Version (every file in dist/release, including SHA256SUMS.txt.sig) --title `"Wardline $Version`" --notes `"See docs/INSTALL.md`""
