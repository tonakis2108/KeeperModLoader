param()
$ErrorActionPreference = "Stop"
$repository = Split-Path -Parent $PSScriptRoot
$build = Join-Path $repository "build-csharp"
$framework = Get-ChildItem "$env:WINDIR\Microsoft.NET\Framework64" -Directory |
    Where-Object { Test-Path (Join-Path $_.FullName "csc.exe") } | Sort-Object Name -Descending | Select-Object -First 1
$csc = Join-Path $framework.FullName "csc.exe"
$compatibility = Join-Path $repository "runtime-package/compatibility"
$runtime = Join-Path $compatibility "runtime/BepInEx"
$tools = Join-Path $compatibility "tools"
$upstream = Join-Path $compatibility "upstream"
New-Item -ItemType Directory -Force $runtime, $tools, $upstream | Out-Null
$archive = Join-Path $upstream "BepInEx_win_x64_5.4.23.5.zip"
$url = "https://github.com/BepInEx/BepInEx/releases/download/v5.4.23.5/BepInEx_win_x64_5.4.23.5.zip"
$expected = "82f9878551030f54657792c0740d9d51a09500eeae1fba21106b0c441e6732c4"
Invoke-WebRequest -Uri $url -OutFile $archive
if ((Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) { throw "Official BepInEx archive failed its pinned digest" }
$extract = Join-Path $build "bepinex-upstream"
Expand-Archive $archive -DestinationPath $extract -Force
Copy-Item (Join-Path $extract "BepInEx/core") $runtime -Recurse -Force
$cecil = Join-Path $runtime "core/Mono.Cecil.dll"
$bep = Join-Path $runtime "core/BepInEx.dll"
if (-not (Test-Path $cecil) -or -not (Test-Path $bep)) { throw "Official release core is incomplete" }

# Compile-only old Unity facade forwards to the existing CI Core facade.
# No Unity facade is shipped in any release package.
$unityFacadeSource = Join-Path $build "UnityEngine.Compatibility.Reference.cs"
@'
using System.Runtime.CompilerServices;
[assembly: TypeForwardedTo(typeof(UnityEngine.MonoBehaviour))]
[assembly: TypeForwardedTo(typeof(UnityEngine.Object))]
[assembly: TypeForwardedTo(typeof(UnityEngine.Component))]
'@ | Set-Content $unityFacadeSource -Encoding UTF8
$unity = Join-Path $build "UnityEngine.dll"
$core = Join-Path $build "UnityEngine.CoreModule.dll"
& $csc /nologo /target:library "/out:$unity" "/reference:$core" $unityFacadeSource
if ($LASTEXITCODE -ne 0) { throw "Unity compatibility facade compilation failed" }
$hostDirectory = Join-Path $runtime "plugins/KeeperLoaderManaged/keeperloader.nativehost"
New-Item -ItemType Directory -Force $hostDirectory | Out-Null
& $csc /nologo /target:library "/out:$hostDirectory/KeeperLoader.NativeHost.dll" "/reference:$bep" "/reference:$unity" "/reference:$core" (Join-Path $repository "src/Compatibility/NativeHost.cs")
if ($LASTEXITCODE -ne 0) { throw "Native compatibility host compilation failed" }
$inspector = Join-Path $tools "KeeperLoader.PackageInspector.exe"
& $csc /nologo /target:exe "/out:$inspector" "/reference:$cecil" /reference:System.Web.Extensions.dll (Join-Path $repository "src/Compatibility/PackageInspector.cs")
if ($LASTEXITCODE -ne 0) { throw "Metadata inspector compilation failed" }
Copy-Item $cecil $tools -Force
Copy-Item (Join-Path $repository "compatibility-licenses") (Join-Path $compatibility "licenses") -Recurse -Force
Copy-Item (Join-Path $repository "THIRD_PARTY_LICENSES.txt") (Join-Path $compatibility "licenses/UnityDoorstop-and-existing-notices.txt") -Force
Copy-Item (Join-Path $repository "COMPATIBILITY.md") (Join-Path $compatibility "NOTICE.txt") -Force

# Scan a compiled fixture without running its attribute constructors.
$fixture = Join-Path $build "inspector-fixture"
New-Item -ItemType Directory -Force $fixture | Out-Null
$fixtureSource = Join-Path $build "InspectorFixture.cs"
@'
using System;
using System.IO;
using BepInEx;
public sealed class ExplosiveAttribute : Attribute {
    public ExplosiveAttribute() { throw new InvalidOperationException("Attribute code must never execute during inspection"); }
}
[BepInPlugin("fixture.plugin", "Fixture", "1.0.0")]
[BepInDependency("dependency.example", "1.2.0")]
[BepInProcess("Graveyard Keeper.exe")]
[Explosive]
public sealed class FixturePlugin : BaseUnityPlugin {
    private void Awake() { throw new InvalidOperationException("Plugin code must never execute during inspection"); }
}
'@ | Set-Content $fixtureSource -Encoding UTF8
& $csc /nologo /target:library "/out:$fixture/Fixture.dll" "/reference:$bep" "/reference:$unity" "/reference:$core" $fixtureSource
if ($LASTEXITCODE -ne 0) { throw "Inspector fixture compilation failed" }
$report = & $inspector $fixture
if ($LASTEXITCODE -ne 0) { throw "Inspector failed its untrusted-code fixture" }
$metadata = $report | ConvertFrom-Json
if ($metadata.native -or $metadata.plugins.Count -ne 1 -or $metadata.plugins[0].id -ne "fixture.plugin" -or
    $metadata.plugins[0].dependencies[0].minimumVersion -ne "1.2.0") { throw "Inspector returned incorrect metadata" }

# Only the optional manager payload gains compatibility tools. The native-only
# runtime ZIP was produced earlier and remains executable-free.
$package = Join-Path $repository "runtime-package"
$checksumPath = Join-Path $package "SHA256SUMS.txt"
$lines = Get-ChildItem $package -Recurse -File | Where-Object { $_.FullName -ne $checksumPath } | Sort-Object FullName | ForEach-Object {
    $relative = $_.FullName.Substring($package.Length + 1).Replace('\', '/')
    "$((Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant())  $relative"
}
$lines | Set-Content $checksumPath -Encoding ASCII
