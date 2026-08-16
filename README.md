# KeeperLoader 0.5.0 Alpha

KeeperLoader is a compact, general-purpose mod loader for **Windows Unity Mono games**. It uses UnityDoorstop to enter the game's existing Mono runtime, waits for managed game code to load, attaches one persistent Unity host, and then loads compatible KeeperLoader mods.

## Supported game profile

KeeperLoader detects the Unity player rather than relying on a hard-coded game name.

Supported:

- Windows 10 or Windows 11 on an x64 PC.
- Windows desktop Unity players with the standard `Game.exe` and `Game_Data` layout.
- Unity **Mono** scripting backend.
- x86 and x64 executables.
- Modular `UnityEngine.*.dll` layouts and older monolithic `UnityEngine.dll` layouts.
- Games whose code is in `Assembly-CSharp.dll` or custom managed assembly-definition DLLs.

Explicitly unsupported in this release:

- Unity IL2CPP games. IL2CPP turns managed game code into native code and requires a different CoreCLR/native-interoperability runtime.
- Linux, macOS, consoles, UWP, and ARM64.
- Plugins built for other mod-loader APIs; KeeperLoader uses its own API and lifecycle.
- Anti-cheat-protected or competitive online games. Do not inject a mod loader where a game's rules prohibit modification.

Unity documents the Windows player as an executable paired with a `ProjectName_Data` directory. Unity also distinguishes Mono's managed JIT runtime from IL2CPP's ahead-of-time native pipeline. UnityDoorstop supports entry into both, but its IL2CPP path requires a separate CoreCLR environment; KeeperLoader 0.5.0 deliberately accepts only the shared Mono path.

KeeperLoader retains the deferred initialization introduced in 0.2.1: it creates its Unity host only after the first `SceneManager.sceneLoaded` callback. This avoids invoking `GameObject` APIs during Doorstop's early assembly-loading phase, which could stall startup on a black screen.

## Universal manager

1. Close any games you intend to manage.
2. Extract this package once and keep it in a convenient location.
3. Run `KeeperLoader-Manager.exe` and approve the Windows administrator prompt.
4. Let it scan Steam libraries or use **Add game...** for another location.
5. Select compatible games (Ctrl-click for several), then choose **Enable / update selected**.
6. Use **Disable selected** to restore their previous bootstrap files later.

The alpha executable is not digitally signed, so Windows SmartScreen may display an **Unknown publisher** warning. Confirm that the file came from the original KeeperLoader package and compare it with `SHA256SUMS.txt` before allowing it. Internet access is required the first time the manager downloads the pinned UnityDoorstop bootstrap.

The same compiled manager controls all detected games in one batch. **Manage mods...** opens ZIP installation, mod listing, package creation, and recoverable removal controls for one highlighted game. **Open saves** opens the detected persistent-data location.

Each game process still requires a small local Doorstop bridge and core compiled against that game's Unity assemblies. The Universal Manager creates and maintains those files automatically; users do not run separate installers or keep separate setup packages.

When KeeperLoader activates, a small green **KL check-mark** badge appears in the upper-right corner for 30 seconds. It remains available on scenes whose names identify them as menu, title, frontend, or lobby screens. Hovering over it displays **KeeperLoader activated**. If crash-recovery safe mode is active, the badge changes to an amber **KL !** and reports that mods are paused. The badge is rendered by the loader and requires no external image file.

The manager remembers detected and manually added game locations in `%LOCALAPPDATA%\KeeperLoader\manager.json`. Replacing or updating the manager executable does not remove this file.

The manager:

- Matches an executable to its corresponding `_Data` directory.
- Detects Mono versus IL2CPP and refuses unsupported targets.
- Reads the executable's PE architecture and selects the matching x86/x64 UnityDoorstop proxy.
- Compiles KeeperLoader against that game's own Unity assemblies.
- Backs up existing bootstrap files before activating its own.
- Records a normalized game ID derived from the executable name.
- Runs as a native Windows GUI executable without invoking CMD or PowerShell.
- Pins the official UnityDoorstop 4.5.0 Windows release to its published SHA-256 digest before use.

## Updating KeeperLoader without losing data

KeeperLoader separates the manager, installed loader core, mods, configuration, state, logs, backups, and game saves. Updating one component does not replace the others.

To update the manager from 0.5.0 onward:

1. Download the newer **KeeperLoader-Windows-x64** artifact ZIP from the repository's successful GitHub Actions build.
2. Open the existing manager and select **Install manager update...**.
3. Select the artifact ZIP without extracting it.
4. The manager requires `VERSION`, `SHA256SUMS.txt`, and `KeeperLoader-Manager.exe`, verifies that the version is newer, checks the executable's SHA-256 digest, then closes, replaces itself, and restarts without CMD or PowerShell.

The first upgrade from 0.4.1 to 0.5.0 is manual because 0.4.1 predates the integrated updater: replace the old manager executable with the one from the 0.5.0 artifact. No game data is stored beside that executable.

After updating the manager, select enabled games that display **Update available**, close those games, and choose **Enable / update selected**. New core DLLs are staged and activated transactionally. The previous core moves to `KeeperLoader\backup\core`; failure restores it automatically. The operation leaves these directories untouched:

- `KeeperLoader\mods`
- `KeeperLoader\config`
- `KeeperLoader\state` except for the loader's version record
- `KeeperLoader\logs`
- `KeeperLoader\backup`
- Unity save-data directories

## Installing game-compatible mod ZIPs

1. Close the selected game.
2. In Universal Manager, highlight it and select **Manage mods...**.
3. Select **Install Mod ZIP...** in its game-management window.
4. Choose a KeeperLoader mod package.
5. Restart the game after installation.

## Updating and restoring mods

To update an installed mod, close the game, select the mod in **Manage mods...**, choose **Update selected from ZIP...**, and select its newer package. KeeperLoader requires the same mod ID and a strictly newer version, then performs the complete package, hash, backend, loader-version, and game-ID validation again.

The active mod folder moves to `KeeperLoader\backup\mods` before the new version is activated. Configuration in `KeeperLoader\config`, state in `KeeperLoader\state\<mod-id>`, and game saves remain untouched. If the updated mod misbehaves, use **Restore previous** while the game is closed; the replaced version is also backed up, so rollback remains reversible.

Mods without a newer ZIP can continue using **Install Mod ZIP...** for deliberate installation or replacement. The dedicated update action rejects an unrelated mod, the same version, and downgrades.

A package must declare:

- `unityBackend: "Mono"`
- `supportedGames`, containing one or more explicit game IDs
- A minimum KeeperLoader version
- Every payload file and its SHA-256 hash

Wildcard compatibility is rejected. The game-management window displays the selected game's ID. A Graveyard Keeper-specific mod declares `"graveyard-keeper"`; installation is rejected for Subnautica, The Forest, or any other game ID.

KeeperLoader also writes the selected game ID and `compatibility=explicit-game-id` into the per-game activation record. At runtime, a mod is loaded only when that record matches the current executable's normalized game ID. Mods activated by earlier manager versions must be reinstalled from their original ZIP once after upgrading to 0.4.1 so they receive the strict activation record.

## Shared features across Unity Mono games

KeeperLoader can provide the same loader-level facilities across supported games:

- Main-thread `OnLoad`, `OnUpdate`, `OnGUI`, and `OnUnload` callbacks
- Unity API access
- Mod discovery and dependency ordering
- Per-mod logging, configuration, and state directories
- Exception isolation and automatic disabling after repeated callback failures
- Crash-recovery safe mode
- Validated ZIP installation, listing, updates, backup, and recoverable uninstall

Game behavior is not standardized. Player types, inventories, maps, quests, save formats, and camera logic differ between games. A loader may be portable while a minimap or gameplay mod is not. Package compatibility declarations prevent that distinction from becoming an expensive surprise.

## Package integrity and recovery

Before installation, KeeperLoader checks archive paths, duplicate entries, expanded size, blocked script/executable types, the complete declared payload, every SHA-256 digest, loader/backend compatibility, and supported game IDs. Source packages are compiled locally against the selected game's Unity assemblies.

After validation, the manager writes a game-specific activation record. The runtime rechecks that record and skips unmanaged, partially installed, or wrong-game mod folders.

Installation is staged. Updating a mod moves the previous version to `KeeperLoader\backup\mods` before activation, and rollback is available from the mod manager.

These checks detect corruption and manifest/payload mismatches. They do not authenticate a publisher or prove that mod code is safe. Only install packages from sources you trust.

## Listing and uninstalling mods

The selected game's management window lists active mods by name, version, and ID. **Uninstall selected** moves the active mod folder to `KeeperLoader\backup\uninstalled`.

KeeperLoader leaves mod configuration, mod state, and game save files untouched. This makes the file operation recoverable, but it cannot make a save independent of content previously written by a mod.

## Persistent data and saves

Unity's standard Windows `Application.persistentDataPath` normally lives under:

`%USERPROFILE%\AppData\LocalLow\<company>\<product>`

**Open save-data location** opens the known exact Graveyard Keeper folder when applicable; for other games it opens the Unity `LocalLow` root because company/product names are not reliably derivable from the executable alone. KeeperLoader never edits files there.

## Creating compatible mod packages

Build against:

- `KeeperLoader\core\KeeperLoader.API.dll`
- Unity assemblies from the selected game's `Game_Data\Managed` folder
- Game assemblies only when the mod actually uses game-specific types

Open **Manage mods...**, choose **Build Mod ZIP...**, select the compiled release folder, and enter the mod ID, name, version, supported game IDs, and output path. The manager creates the manifest and hashes every payload file.

List every supported game ID explicitly. See `DEVELOPING_MODS.md`.

## Installed layout

```text
Game Folder/
├── Game.exe
├── Game_Data/
├── winhttp.dll
├── doorstop_config.ini
└── KeeperLoader/
    ├── core/
    ├── mods/
    ├── config/
    ├── logs/
    ├── state/
    └── backup/
```

## Safe mode and removal

KeeperLoader writes a boot marker at startup and removes it on clean shutdown. Following an unclean exit, the next run skips mods. Close that safe-mode run normally and restart to load mods again.

If the game cannot reach its first scene, rename the game-folder `winhttp.dll` to `winhttp.keeperloader-disabled.dll` to disable injection immediately. This does not touch mods, configurations, state, or saves. The manager can then restore the pre-KeeperLoader bootstrap files.

Select the game in `KeeperLoader-Manager.exe` and choose **Disable selected** to restore the most recently backed-up bootstrap files. The `KeeperLoader` directory remains so mods, logs, configurations, and backups are not destroyed automatically.

## Current limitations

- Alpha software; test each game separately and back up important saves.
- Runtime integration cannot be tested here against every commercial Unity build.
- No IL2CPP runtime, compatibility shim for third-party mod loaders, global patching framework, or live assembly unloading.
- Some games use custom launchers, native wrappers, DRM, or anti-cheat systems that can prevent Doorstop injection.

KeeperLoader itself does not write to game saves.

## Technical references and licensing

- [Unity Windows Player build binaries](https://docs.unity3d.com/2019.4/Documentation/Manual/WindowsStandaloneBinaries.html)
- [Unity scripting backends](https://docs.unity3d.com/Manual/scripting-backends.html)
- [Unity persistent data path](https://docs.unity3d.com/ScriptReference/Application-persistentDataPath.html)
- [UnityDoorstop](https://github.com/NeighTools/UnityDoorstop)

The manager downloads the official UnityDoorstop 4.5.0 Windows release and verifies its published SHA-256 digest. UnityDoorstop is LGPL-2.1 licensed. KeeperLoader is licensed under LGPL-2.1-or-later; see `LICENSE` and `THIRD_PARTY.md`.
