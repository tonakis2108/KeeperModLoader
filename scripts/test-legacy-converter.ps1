$ErrorActionPreference = "Stop"
$repository = Split-Path -Parent $PSScriptRoot
$work = Join-Path ([System.IO.Path]::GetTempPath()) ("KeeperLoader-ConverterTest-" + [guid]::NewGuid().ToString("N"))
$sourcePackage = Join-Path $work "legacy.zip"
$convertedPackage = Join-Path $work "converted.zip"
$packageRoot = Join-Path $work "package"
$sourceRoot = Join-Path $packageRoot "src"
$expanded = Join-Path $work "expanded"

try {
    New-Item -ItemType Directory -Force $sourceRoot | Out-Null
    $source = @'
using KeeperLoader.API;

[KeeperMod("test.legacy-converter", "Legacy Converter Test", "1.0.0")]
public sealed class LegacyConverterTest : KeeperMod
{
    public override void OnLoad(KeeperContext context) { }
}
'@
    $sourcePath = Join-Path $sourceRoot "LegacyConverterTest.cs"
    [System.IO.File]::WriteAllText($sourcePath, $source, (New-Object System.Text.UTF8Encoding($false)))
    $digest = (Get-FileHash -LiteralPath $sourcePath -Algorithm SHA256).Hash.ToLowerInvariant()
    $manifest = @{
        id = "test.legacy-converter"
        name = "Legacy Converter Test"
        version = "1.0.0"
        minimumKeeperLoaderVersion = "0.6.1"
        unityBackend = "Mono"
        supportedGames = @("graveyard-keeper")
        files = @(@{ path = "src/LegacyConverterTest.cs"; sha256 = $digest })
        build = @{ sources = @("src/LegacyConverterTest.cs"); output = "Legacy.Converter.Test.dll" }
    }
    $manifestJson = $manifest | ConvertTo-Json -Depth 10
    [System.IO.File]::WriteAllText((Join-Path $packageRoot "keepermod.json"), $manifestJson, (New-Object System.Text.UTF8Encoding($false)))
    Compress-Archive -Path (Join-Path $packageRoot '*') -DestinationPath $sourcePackage

    & (Join-Path $PSScriptRoot "convert-legacy-mod.ps1") `
        -Package $sourcePackage `
        -ManagedDirectory (Join-Path $repository "build-csharp") `
        -OutputPackage $convertedPackage

    Expand-Archive -LiteralPath $convertedPackage -DestinationPath $expanded
    $compiled = Join-Path $expanded "Legacy.Converter.Test.dll"
    if (-not (Test-Path -LiteralPath $compiled -PathType Leaf)) {
        throw "Converted package is missing its compiled DLL"
    }
    $convertedManifest = Get-Content (Join-Path $expanded "keepermod.json") -Raw | ConvertFrom-Json
    $entry = $convertedManifest.files | Where-Object { $_.path -eq "Legacy.Converter.Test.dll" } | Select-Object -First 1
    if ($null -eq $entry) { throw "Converted manifest does not declare the compiled DLL" }
    $compiledDigest = (Get-FileHash -LiteralPath $compiled -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($compiledDigest -ne ([string]$entry.sha256).ToLowerInvariant()) {
        throw "Converted manifest contains the wrong compiled DLL hash"
    }
}
finally {
    if (Test-Path -LiteralPath $work) { Remove-Item -LiteralPath $work -Recurse -Force }
}
