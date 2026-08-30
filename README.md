# KeeperLoader 0.7.0 Alpha

KeeperLoader is a native managed mod loader for the Windows Steam version of **Graveyard Keeper**. Version 0.7.0 separates the small game runtime from the optional Windows manager so Nexus users can download an executable-free main file.

## Choose a download

### Recommended Nexus/manual runtime

`KeeperLoader-GraveyardKeeper-Runtime-v0.7.0.zip`

- Precompiled specifically for Graveyard Keeper's Unity Mono runtime.
- Contains no `.exe`, downloader, updater, compiler, script, or external-plugin loader.
- Contains the three managed KeeperLoader DLLs and the official unmodified UnityDoorstop 4.5.0 x64 proxy.
- Can be extracted directly beside `Graveyard Keeper.exe`.
- Preserves `KeeperLoader\mods`, configuration, state, logs, backups, and game saves when a newer runtime ZIP is copied over it.

### Optional manager

`KeeperLoader-Manager-Windows-x64-v0.7.0.zip`

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

The manager is distributed separately because it is an unsigned Windows executable with legitimate file-management, process-checking, network-download, and self-update behavior. Those capabilities are useful, but they also resemble behaviors heuristic antivirus engines inspect. They are not present in the Nexus runtime ZIP.

## Requirements

- Graveyard Keeper for Windows through Steam, App ID `599140`.
- Current x64 Unity Mono game build.
- Windows 10 or Windows 11.
- The game must be closed during installation, updates, or removal.

KeeperLoader 0.7.0 intentionally does not target other games, IL2CPP builds, macOS, Linux, consoles, ARM64, external plugin formats, or third-party runtime emulation.

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
4. Select Graveyard Keeper and choose **Enable / update selected** to update the game-local runtime.

## Installing and updating native mods

Existing native KeeperLoader mods do not need source or package changes for 0.7.0. The public `KeeperLoader.API` source is byte-for-byte unchanged from 0.6.2, and CI rejects the release if that compatibility boundary changes.

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
- Compile-only Unity facades are used only to build against Graveyard Keeper's known Unity API surface and are explicitly forbidden from the distributed ZIP.
- The official UnityDoorstop archive is pinned to SHA-256 `7bb953e8d883c8bde76ced96f6d0e45660ad6e0151880d8ab5856bf4f532b147` before its x64 proxy is copied unchanged.
- `SHA256SUMS.txt` records every file in the runtime package.
- CI checks that the runtime ZIP contains no executable file and scans both runtime and optional manager payloads with Microsoft Defender before release publication.

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
