param([string]$OutputDirectory = "dist")

$ErrorActionPreference = "Stop"
$repository = Split-Path -Parent $PSScriptRoot
$version = (Get-Content (Join-Path $repository "VERSION") -Raw).Trim()
$package = Join-Path $repository "manager-package"
$output = Join-Path $repository $OutputDirectory
New-Item -ItemType Directory -Force $package, $output | Out-Null

$files = @(
    "KeeperLoader-Manager.exe",
    "VERSION",
    "README.md",
    "DEVELOPING_MODS.md",
    "LICENSE",
    "THIRD_PARTY.md",
    "THIRD_PARTY_LICENSES.txt"
)
foreach ($name in $files) {
    Copy-Item (Join-Path $repository $name) (Join-Path $package $name) -Force
}
$manager = Join-Path $package "KeeperLoader-Manager.exe"
$hash = (Get-FileHash $manager -Algorithm SHA256).Hash.ToLowerInvariant()
"$hash  KeeperLoader-Manager.exe" | Set-Content (Join-Path $package "SHA256SUMS.txt") -Encoding ASCII

$archive = Join-Path $output "KeeperLoader-Manager-Windows-x64-v$version.zip"
if (Test-Path $archive) { Remove-Item $archive -Force }
Compress-Archive -Path (Join-Path $package "*") -DestinationPath $archive -CompressionLevel Optimal
Write-Output $archive
