param(
    [string]$OutputDirectory = "dist",
    [switch]$SkipDoorstopDownload
)

$ErrorActionPreference = "Stop"
$repository = Split-Path -Parent $PSScriptRoot
$version = (Get-Content (Join-Path $repository "VERSION") -Raw).Trim()
$build = Join-Path $repository "build-csharp"
$package = Join-Path $repository "runtime-package"
$output = Join-Path $repository $OutputDirectory

$framework = Get-ChildItem "$env:WINDIR\Microsoft.NET\Framework64" -Directory |
    Sort-Object Name -Descending |
    Where-Object { Test-Path (Join-Path $_.FullName "csc.exe") } |
    Select-Object -First 1
if (-not $framework) { throw "A .NET Framework C# compiler was not found" }
$csc = Join-Path $framework.FullName "csc.exe"

New-Item -ItemType Directory -Force $build, $package, $output | Out-Null

$api = Join-Path $build "KeeperLoader.API.dll"
$bootstrap = Join-Path $build "KeeperLoader.Bootstrap.dll"
$coreReference = Join-Path $build "UnityEngine.CoreModule.dll"
$textReference = Join-Path $build "UnityEngine.TextRenderingModule.dll"
$imguiReference = Join-Path $build "UnityEngine.IMGUIModule.dll"
$runtime = Join-Path $build "KeeperLoader.Runtime.dll"

& $csc /nologo /target:library "/out:$api" (Join-Path $repository "src\API\KeeperLoaderApi.cs")
if ($LASTEXITCODE -ne 0) { throw "KeeperLoader API compilation failed" }
& $csc /nologo /target:library "/out:$bootstrap" (Join-Path $repository "src\Bootstrap\Entrypoint.cs")
if ($LASTEXITCODE -ne 0) { throw "KeeperLoader bootstrap compilation failed" }
& $csc /nologo /target:library "/out:$coreReference" (Join-Path $repository "src\Runtime.Tests\UnityEngine.CoreModule.Reference.cs")
if ($LASTEXITCODE -ne 0) { throw "UnityEngine Core compile facade failed" }
& $csc /nologo /target:library "/out:$textReference" (Join-Path $repository "src\Runtime.Tests\UnityEngine.TextRenderingModule.Reference.cs")
if ($LASTEXITCODE -ne 0) { throw "UnityEngine text compile facade failed" }
& $csc /nologo /target:library "/out:$imguiReference" "/reference:$coreReference" "/reference:$textReference" (Join-Path $repository "src\Runtime.Tests\UnityEngine.IMGUIModule.Reference.cs")
if ($LASTEXITCODE -ne 0) { throw "UnityEngine IMGUI compile facade failed" }
$runtimeSources = Get-ChildItem (Join-Path $repository "src\Runtime\*.cs") | ForEach-Object FullName
& $csc /nologo /target:library "/out:$runtime" "/reference:$api" "/reference:$coreReference" "/reference:$textReference" "/reference:$imguiReference" $runtimeSources
if ($LASTEXITCODE -ne 0) { throw "KeeperLoader runtime compilation failed" }

$smokeSource = Join-Path $build "RuntimeHostSmoke.cs"
$smokeExecutable = Join-Path $build "RuntimeHostSmoke.exe"
@'
using KeeperLoader.Runtime;
using UnityEngine;

internal static class RuntimeHostSmoke
{
    private static int Main()
    {
        RuntimeHost.Attach();
        if (GameObject.Find("KeeperLoader Runtime") != null) return 1;
        Application.RaiseBeforeRender();
        if (GameObject.Find("KeeperLoader Runtime") == null) return 2;
        Camera.RaisePreCull();
        return 0;
    }
}
'@ | Set-Content $smokeSource -Encoding UTF8
& $csc /nologo /target:exe "/out:$smokeExecutable" "/reference:$runtime" "/reference:$api" "/reference:$coreReference" "/reference:$textReference" "/reference:$imguiReference" $smokeSource
if ($LASTEXITCODE -ne 0) { throw "KeeperLoader runtime-host smoke test compilation failed" }
& $smokeExecutable
if ($LASTEXITCODE -ne 0) { throw "KeeperLoader runtime host was not deferred into the Unity frame loop" }

$runtimeHostSource = Get-Content (Join-Path $repository "src\Runtime\RuntimeHost.cs") -Raw
foreach ($forbidden in @("using UnityEngine.SceneManagement", "SceneManager.sceneLoaded", "SceneManager.GetActiveScene")) {
    if ($runtimeHostSource.Contains($forbidden)) {
        throw "Runtime reintroduced a hard dependency on an optional Unity scene API: $forbidden"
    }
}
foreach ($required in @("onBeforeRender", "onPreCull", "Unity frame loop detected")) {
    if (-not $runtimeHostSource.Contains($required)) {
        throw "Runtime is missing its deferred Unity startup contract: $required"
    }
}
if ($runtimeHostSource.Contains("host.hideFlags")) {
    throw "Runtime reintroduced the incompatible GameObject.hideFlags member reference"
}

$apiBlob = (git -C $repository hash-object "src/API/KeeperLoaderApi.cs").Trim()
if ($apiBlob -ne "0cc3c28cdb6cb51a88709c5b23e45fb40f5cac72") {
    throw "KeeperLoader API changed; native mod compatibility review is required"
}

$core = Join-Path $package "KeeperLoader\core"
$state = Join-Path $package "KeeperLoader\state"
foreach ($directory in @($core, $state, (Join-Path $package "KeeperLoader\mods"), (Join-Path $package "KeeperLoader\config"), (Join-Path $package "KeeperLoader\logs"))) {
    New-Item -ItemType Directory -Force $directory | Out-Null
}
Copy-Item $api, $bootstrap, $runtime -Destination $core -Force
Copy-Item (Join-Path $repository "distribution\doorstop_config.ini") -Destination $package -Force
Copy-Item (Join-Path $repository "distribution\README-RUNTIME.txt"), (Join-Path $repository "LICENSE"), (Join-Path $repository "THIRD_PARTY.md") -Destination $package -Force
$gameRecord = @{
    gameId = "graveyard-keeper"
    executable = "Graveyard Keeper.exe"
    backend = "Mono"
    architecture = "x64"
    dataDirectory = "Graveyard Keeper_Data"
    managedDirectory = "Graveyard Keeper_Data\\Managed"
    keeperLoaderVersion = $version
} | ConvertTo-Json
$utf8WithoutBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText((Join-Path $state "game.json"), $gameRecord + "`n", $utf8WithoutBom)

if (-not $SkipDoorstopDownload) {
    $doorstopUrl = "https://github.com/NeighTools/UnityDoorstop/releases/download/v4.5.0/doorstop_win_release_4.5.0.zip"
    $doorstopDigest = "7bb953e8d883c8bde76ced96f6d0e45660ad6e0151880d8ab5856bf4f532b147"
    $doorstopArchive = Join-Path $build "doorstop_win_release_4.5.0.zip"
    Invoke-WebRequest -Uri $doorstopUrl -OutFile $doorstopArchive
    $actual = (Get-FileHash $doorstopArchive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $doorstopDigest) { throw "UnityDoorstop archive failed its pinned SHA-256 check" }
    $doorstopExtract = Join-Path $build "doorstop"
    Expand-Archive $doorstopArchive -DestinationPath $doorstopExtract -Force
    $proxy = Get-ChildItem $doorstopExtract -Recurse -File -Filter winhttp.dll |
        Where-Object { $_.FullName -match '(?i)(^|[\\/])(x64|win64)([\\/]|$)' } |
        Select-Object -First 1
    if (-not $proxy) { throw "The official UnityDoorstop archive has no x64 winhttp.dll" }
    Copy-Item $proxy.FullName (Join-Path $package "winhttp.dll") -Force
}

if (-not (Test-Path (Join-Path $package "winhttp.dll"))) {
    throw "The runtime package is missing winhttp.dll"
}
if (Get-ChildItem $package -Recurse -File -Filter *.exe) {
    throw "The Nexus runtime package must not contain executable files"
}
foreach ($facade in @("UnityEngine.CoreModule.dll", "UnityEngine.TextRenderingModule.dll", "UnityEngine.IMGUIModule.dll")) {
    if (Get-ChildItem $package -Recurse -File -Filter $facade) {
        throw "Compile-only Unity facade $facade must never be distributed"
    }
}

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

$archive = Join-Path $output "KeeperLoader-GraveyardKeeper-Runtime-v$version.zip"
if (Test-Path $archive) { Remove-Item $archive -Force }
Compress-Archive -Path (Join-Path $package "*") -DestinationPath $archive -CompressionLevel Optimal
Write-Output $archive
