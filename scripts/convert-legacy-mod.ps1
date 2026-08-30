param(
    [Parameter(Mandatory = $true)]
    [string]$Package,

    [Parameter(Mandatory = $true)]
    [string]$ManagedDirectory,

    [Parameter(Mandatory = $true)]
    [string]$OutputPackage
)

$ErrorActionPreference = "Stop"
$repository = Split-Path -Parent $PSScriptRoot
$packagePath = (Resolve-Path $Package).Path
$managedPath = (Resolve-Path $ManagedDirectory).Path
$outputPath = [System.IO.Path]::GetFullPath($OutputPackage)
$work = Join-Path ([System.IO.Path]::GetTempPath()) ("KeeperLoader-LegacyMod-" + [guid]::NewGuid().ToString("N"))
$extract = Join-Path $work "package"
$build = Join-Path $work "build"

function Get-NormalizedPackagePath([string]$Path) {
    $value = $Path.Replace('\', '/')
    if ($value.StartsWith('./')) { $value = $value.Substring(2) }
    $value = $value.TrimEnd([char[]]'/')
    if ([string]::IsNullOrWhiteSpace($value) -or $value.StartsWith('/') -or $value.Contains(':')) {
        throw "Unsafe package path: $Path"
    }
    foreach ($part in $value.Split('/')) {
        if ([string]::IsNullOrWhiteSpace($part) -or $part -eq '.' -or $part -eq '..') {
            throw "Unsafe package path: $Path"
        }
    }
    return $value
}

try {
    New-Item -ItemType Directory -Force $extract, $build | Out-Null
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive = [System.IO.Compression.ZipFile]::OpenRead($packagePath)
    try {
        if ($archive.Entries.Count -gt 2048) { throw "The package contains more than 2048 entries" }
        $seen = @{}
        [uint64]$expandedSize = 0
        foreach ($entry in $archive.Entries) {
            $relative = Get-NormalizedPackagePath $entry.FullName
            $key = $relative.ToLowerInvariant()
            if ($seen.ContainsKey($key)) { throw "Duplicate package path: $relative" }
            $seen[$key] = $true
            $expandedSize += [uint64]$entry.Length
            if ($expandedSize -gt 268435456) { throw "Expanded package content exceeds 256 MB" }
        }
    }
    finally {
        $archive.Dispose()
    }
    Expand-Archive -LiteralPath $packagePath -DestinationPath $extract

    $manifestPath = Join-Path $extract "keepermod.json"
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
        throw "keepermod.json is missing from the ZIP root"
    }
    $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    if ($null -eq $manifest.build -or $manifest.build.sources.Count -eq 0) {
        throw "The package has no legacy source build specification"
    }
    if ($manifest.build.output -notmatch '^[A-Za-z0-9._-]+\.dll$') {
        throw "The legacy build output name is invalid"
    }

    $declared = @{}
    foreach ($file in $manifest.files) {
        $relative = Get-NormalizedPackagePath $file.path
        $full = Join-Path $extract ($relative.Replace('/', [System.IO.Path]::DirectorySeparatorChar))
        if (-not (Test-Path -LiteralPath $full -PathType Leaf)) {
            throw "Declared file is missing: $relative"
        }
        $actual = (Get-FileHash -LiteralPath $full -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne ([string]$file.sha256).ToLowerInvariant()) {
            throw "SHA-256 mismatch for $relative"
        }
        $key = $relative.ToLowerInvariant()
        if ($declared.ContainsKey($key)) { throw "Duplicate manifest path: $relative" }
        $declared[$key] = $full
    }
    foreach ($payload in Get-ChildItem -LiteralPath $extract -Recurse -File) {
        $relative = $payload.FullName.Substring($extract.Length + 1).Replace('\', '/')
        if ($relative -eq 'keepermod.json') { continue }
        if (-not $declared.ContainsKey($relative.ToLowerInvariant())) {
            throw "Undeclared file is present: $relative"
        }
    }

    $sources = @()
    foreach ($source in $manifest.build.sources) {
        $relative = Get-NormalizedPackagePath ([string]$source)
        if (-not $relative.EndsWith('.cs', [System.StringComparison]::OrdinalIgnoreCase)) {
            throw "Build source is not a C# file: $relative"
        }
        $key = $relative.ToLowerInvariant()
        if (-not $declared.ContainsKey($key)) { throw "Build source is not declared and hashed: $relative" }
        $sources += $declared[$key]
    }

    $framework = Get-ChildItem "$env:WINDIR\Microsoft.NET\Framework64" -Directory |
        Sort-Object Name -Descending |
        Where-Object { Test-Path (Join-Path $_.FullName "csc.exe") } |
        Select-Object -First 1
    if (-not $framework) { throw "A Windows .NET Framework C# compiler was not found" }
    $csc = Join-Path $framework.FullName "csc.exe"

    $api = Join-Path $build "KeeperLoader.API.dll"
    & $csc /nologo /target:library "/out:$api" (Join-Path $repository "src\API\KeeperLoaderApi.cs")
    if ($LASTEXITCODE -ne 0) { throw "KeeperLoader API compilation failed" }

    $references = @($api)
    $references += Get-ChildItem -LiteralPath $managedPath -File -Filter "UnityEngine*.dll" |
        Sort-Object Name | ForEach-Object FullName
    if ($references.Count -le 1) { throw "No UnityEngine assemblies were found in the Managed directory" }
    foreach ($gameAssembly in @("Assembly-CSharp.dll", "Assembly-CSharp-firstpass.dll")) {
        $candidate = Join-Path $managedPath $gameAssembly
        if (Test-Path -LiteralPath $candidate -PathType Leaf) { $references += $candidate }
    }

    $compiled = Join-Path $extract ([string]$manifest.build.output)
    $arguments = @('/nologo', '/target:library', '/optimize+', "/out:$compiled")
    $arguments += $references | ForEach-Object { "/reference:$_" }
    $arguments += $sources
    & $csc $arguments
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $compiled -PathType Leaf)) {
        throw "Legacy mod compilation failed"
    }

    $outputRelative = Get-NormalizedPackagePath ([string]$manifest.build.output)
    $outputDigest = (Get-FileHash -LiteralPath $compiled -Algorithm SHA256).Hash.ToLowerInvariant()
    $newFiles = @($manifest.files | Where-Object {
        (Get-NormalizedPackagePath ([string]$_.path)).ToLowerInvariant() -ne $outputRelative.ToLowerInvariant()
    })
    $newFiles += [pscustomobject]@{ path = $outputRelative; sha256 = $outputDigest }
    $manifest.files = @($newFiles | Sort-Object { ([string]$_.path).ToLowerInvariant() })
    # The DLL is now the distributable payload. Removing the obsolete instruction
    # also makes the converted ZIP installable by the already released v0.7.2
    # manager, which deliberately rejects any package asking it to compile.
    $manifest.PSObject.Properties.Remove('build')
    $manifest | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $manifestPath -Encoding UTF8

    $parent = Split-Path -Parent $outputPath
    if (-not [string]::IsNullOrWhiteSpace($parent)) { New-Item -ItemType Directory -Force $parent | Out-Null }
    if (Test-Path -LiteralPath $outputPath) { Remove-Item -LiteralPath $outputPath -Force }
    Compress-Archive -Path (Join-Path $extract '*') -DestinationPath $outputPath -CompressionLevel Optimal
    Write-Output $outputPath
}
finally {
    if (Test-Path -LiteralPath $work) { Remove-Item -LiteralPath $work -Recurse -Force }
}
