# Builds everything someone needs to install Wardline, into dist/release:
#   wardline-linux-amd64, wardline-linux-arm64, wardline-windows-amd64.exe
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
    Move-Item "$tmp/wardline" "$out/wardline-$($t.os)-$($t.arch)$($t.ext)"
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

Get-ChildItem $out -File | Where-Object { $_.Name -ne "SHA256SUMS.txt" } | ForEach-Object {
    "{0}  {1}" -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower(), $_.Name
} | Set-Content "$out/SHA256SUMS.txt" -Encoding ascii

Get-ChildItem $out | Format-Table Name, Length
Write-Host "Version: $Version"
Write-Host "To publish once the GitHub repository exists:"
Write-Host "  gh release create $Version dist/release/* --title `"Wardline $Version`" --notes `"See docs/INSTALL.md`""
