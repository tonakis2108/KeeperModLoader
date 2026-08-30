# KeeperLoader 0.7.5 Alpha

KeeperLoader is a native managed mod loader for the Windows Steam version of **Graveyard Keeper**. Version 0.7.5 fixes the precompiled runtime's Unity member contract and defers host creation until Unity has entered its frame loop. This restores native mod loading without changing the KeeperLoader mod API or package format.

## Choose a download

### Clean Nexus/manual runtime

`KeeperLoader-GraveyardKeeper-Runtime-v0.7.5.zip`

- Precompiled specifically for Graveyard Keeper's Unity Mono runtime.
- Contains no `.exe`, downloader, updater, compiler, script, or external-plugin loader.
- Contains the three managed KeeperLoader DLLs and the official unmodified UnityDoorstop 4.5.0 x64 proxy.
- Can be extracted directly beside `Graveyard Keeper.exe`.
- Preserves `KeeperLoader\mods`, configuration, state, logs, backups, and game saves when a newer runtime ZIP is copied over it.

### Easiest installation: optional manager

`KeeperLoader-Manager-Windows-x64-v0.7.5.zip`

The optional manager keeps the convenient workflow from previous releases:

- Find Graveyard Keeper through Steam or a manually selected folder.
- Enable or update the game-local runtime.
- Launch through Steam.
- Install and validate native KeeperLoader mod ZIPs.
- Update mods while preserving settings and state.
- Enable, disable, roll back, or uninstall individual mods.
- Select safe mode for one launch.
- Update the manager from a verified release ZIP.
- Remove KeeperLoader and restore backed-up bootstrap files.
- Install the bundled precompiled runtime without internet access or an on-device compiler.

The complete manager ZIP must be extracted before it is run because the verified `runtime` directory is part of the installation payload. The manager does not download UnityDoorstop and does not compile KeeperLoader on the player's computer. It remains a separate download because it is a Windows executable with legitimate file-management, process-checking, and self-update behavior. Those capabilities are not present in the Nexus runtime ZIP.

**One-time update note for old managers:** Managers v0.7.3 and older incorrectly compare incoming runtime metadata with their own older version and therefore reject a normal version upgrade. Close those managers, extract the complete v0.7.5 ZIP into a new folder, and run its executable directly. Managers v0.7.4 and newer can install this and future versions through **Install manager update...**.

## Requirements

- Graveyard Keeper for Windows through Steam, App ID `599140`.
- Current x64 Unity Mono game build.
- Windows 10 or Windows 11.
- The game must be closed during installation, updates, or removal.

KeeperLoader 0.7.5 intentionally does not target other games, IL2CPP builds, macOS, Linux, consoles, ARM64, external plugin formats, or third-party runtime emulation.

## Easiest setup with the manager

1. Download `KeeperLoader-Manager-Windows-x64-v0.7.5.zip` from the official GitHub release.
2. Extract the complete ZIP into a permanent folder.
3. Run `KeeperLoader-Manager.exe`. Steam scanning starts automatically and selects Graveyard Keeper when it is the only result.
4. Select **Install / update KeeperLoader**.
5. Open **Manage mods…** to install, update, enable, disable, restore, or uninstall native KeeperLoader mod ZIPs.
6. Use **Launch through Steam** or start the game normally.

No internet connection is needed to install the bundled runtime. Internet access is only needed when the user chooses to download a future release.

## Manual runtime installation

1. Close Graveyard Keeper.
2. In Steam, open **Library → Graveyard Keeper → Properties → Installed Files → Browse**.
3. Extract the runtime ZIP directly into that folder, beside `Graveyard Keeper.exe`.
4. Start the game through Steam.
5. A green `KL` badge appears briefly when KeeperLoader activates.

Installed layout:

```text
Graveyard Keeper/
├── Graveyard Keeper.exe
├── Graveyard Keeper_Data/
├── winhttp.dll
├── doorstop_config.ini
└── KeeperLoader/
    ├── core/
    │   ├── KeeperLoader.API.dll
    │   ├── KeeperLoader.Bootstrap.dll
    │   └── KeeperLoader.Runtime.dll
    ├── mods/
    ├── config/
    ├── state/
    ├── logs/
    └── backup/
```

## Easy runtime and manager updates

To update the runtime manually, close the game and extract the newer runtime ZIP into the same game folder. Replace the core DLLs, `winhttp.dll`, and `doorstop_config.ini`. The package does not contain mod, configuration, state, backup, or save payloads, so those remain in place.

With the optional manager:

1. Download the newer manager ZIP without extracting it.
2. In the current manager, select **Install manager update…**.
3. Choose the ZIP. The manager validates `VERSION`, `SHA256SUMS.txt`, and the executable hash before replacing and restarting itself.
4. Select Graveyard Keeper and choose **Install / update KeeperLoader** to update the game-local runtime.

## Installing and updating native mods

Existing compiled native KeeperLoader mods do not need source or API changes for 0.7.5. The public `KeeperLoader.API` source is byte-for-byte unchanged from 0.6.2, and CI rejects the release if that compatibility boundary changes. Legacy ZIPs that contain C# source but no DLL must be converted once on the publisher/build side; KeeperLoader deliberately does not invoke a compiler on a player's PC. The repository converter produces a precompiled package compatible with v0.7.2 and newer managers. Transitional ZIPs that retain legacy `build` metadata are accepted only when the declared, hashed output DLL is included.

Using the optional manager:

1. Close Graveyard Keeper.
2. Select Graveyard Keeper and open **Manage mods…**.
3. Use **Install Mod ZIP…** for a new package.
4. Select an installed mod and use **Update selected from ZIP…** for a newer version.

KeeperLoader validates archive paths, blocked executable/script types, the manifest, explicit `graveyard-keeper` support, minimum loader version, and SHA-256 for every declared payload file. Updates are staged and the previous mod version is retained for rollback. Mod configuration and state are stored outside the active package folder.

The experimental external-plugin loader was removed in 0.6.2 and remains absent. Old external packages are displayed only so the manager can remove them; the runtime never loads them.

## Security and reproducibility

- The complete source and Windows release workflows are public.
- The Nexus runtime package is assembled by GitHub Actions from source.
- The manager release contains the same precompiled runtime in a separately checksummed `runtime` directory; it performs no runtime download and invokes no compiler on the player's PC.
- Compile-only Unity facades are used only to build against Graveyard Keeper's known Unity API surface and are explicitly forbidden from the distributed ZIP.
- The official UnityDoorstop archive is pinned to SHA-256 `7bb953e8d883c8bde76ced96f6d0e45660ad6e0151880d8ab5856bf4f532b147` before its x64 proxy is copied unchanged.
- `SHA256SUMS.txt` records every file in the runtime package.
- CI checks that the runtime ZIP contains no executable file and scans both runtime and optional manager payloads with Microsoft Defender before release publication.
- CI requires every release to build both the clean runtime and the full manager, and keeps the native mod API compatibility check active so a loader update cannot silently require existing mods to change.

No project can guarantee that every future antivirus engine will return zero heuristic detections. Do not disable antivirus protection or create exclusions. If a service reports a detection, compare the exact SHA-256 with the official release, inspect the public workflow, and report the vendor name and detection label.

Native mods are trusted managed code inside the game process. Package integrity checks detect corruption and undeclared payloads; they do not prove that third-party mod code is harmless. Install mods only from publishers you trust.

## Safe mode and removal

Safe mode is never automatic. In the manager, choose **Safe mode next launch** to skip every mod once. The request is consumed when the game starts.

If the game cannot start, rename `winhttp.dll` to `winhttp.keeperloader-disabled.dll` to pause injection. This does not alter mods, configuration, state, or saves.

For complete removal, use **Remove selected** in the manager. It restores backed-up pre-loader bootstrap files when available and removes KeeperLoader-managed files. Graveyard Keeper and its saves are not removed.

## Mod development

Build against `KeeperLoader\core\KeeperLoader.API.dll`, the Unity assemblies under `Graveyard Keeper_Data\Managed`, and `Assembly-CSharp.dll` only when game-specific types are needed. See [DEVELOPING_MODS.md](DEVELOPING_MODS.md) for the lifecycle, package rules, dependencies, configuration, error isolation, and cleanup requirements.

## Licensing

KeeperLoader is licensed under LGPL-2.1-or-later. UnityDoorstop is distributed unmodified under LGPL-2.1. See [LICENSE](LICENSE), [THIRD_PARTY.md](THIRD_PARTY.md), and [THIRD_PARTY_LICENSES.txt](THIRD_PARTY_LICENSES.txt).
