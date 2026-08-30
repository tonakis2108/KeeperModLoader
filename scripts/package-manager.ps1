param([string]$OutputDirectory = "dist")

$ErrorActionPreference = "Stop"
$repository = Split-Path -Parent $PSScriptRoot
$version = (Get-Content (Join-Path $repository "VERSION") -Raw).Trim()
$package = Join-Path $repository "manager-package"
$output = Join-Path $repository $OutputDirectory
if (Test-Path $package) { Remove-Item $package -Recurse -Force }
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

$runtimeSource = Join-Path $repository "runtime-package"
if (-not (Test-Path (Join-Path $runtimeSource "SHA256SUMS.txt"))) {
    throw "Build the executable-free runtime before packaging the manager"
}
$runtimeDestination = Join-Path $package "runtime"
New-Item -ItemType Directory -Force $runtimeDestination | Out-Null
Copy-Item (Join-Path $runtimeSource "*") $runtimeDestination -Recurse -Force

$checksumPath = Join-Path $package "SHA256SUMS.txt"
$lines = Get-ChildItem $package -Recurse -File |
    Where-Object { $_.FullName -ne $checksumPath } |
    Sort-Object FullName |
    ForEach-Object {
        $relative = $_.FullName.Substring($package.Length + 1).Replace('\', '/')
        $digest = (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        "$digest  $relative"
    }
$lines | Set-Content $checksumPath -Encoding ASCII

$archive = Join-Path $output "KeeperLoader-Manager-Windows-x64-v$version.zip"
if (Test-Path $archive) { Remove-Item $archive -Force }
Compress-Archive -Path (Join-Path $package "*") -DestinationPath $archive -CompressionLevel Optimal
Write-Output $archive
