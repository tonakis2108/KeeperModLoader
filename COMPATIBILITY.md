# Optional BepInEx 5 compatibility (0.8.0 preview)

This is an opt-in feature for the Windows x64 Unity Mono Steam version of
Graveyard Keeper. Other games, BepInEx 6, IL2CPP and preloader patchers are
not supported. Existing native KeeperLoader API source and mod packages are
unchanged. Compatibility still requires an in-game regression test before
a stable release can be claimed.

## Use

1. Extract the complete Manager ZIP. Close the game.
2. Install/update KeeperLoader for the selected game.
3. Open Manage mods > Enable / repair BepInEx 5.
4. Read the notices and explicitly accept the optional runtime.
5. Use Install BepInEx ZIP for a mod the publisher lists for Graveyard Keeper.
6. Start the game. Check both KeeperLoader/logs/latest.log and BepInEx/LogOutput.log.

Native packages continue using Install Mod ZIP. The unified mod list labels
each type. Enable/disable, update and restore operate on separate directories.
External disable moves its entire package outside the BepInEx plugin search
directory. Configuration remains under BepInEx/config. External uninstall is
recoverable: it archives the package and preserves configuration/backups.
Loaded status is NOT inferred from installed/enabled status. BepInEx can skip
plugins or report startup failures; inspect the current launch's log.

Restore native mode disables compatibility without deleting its files or
configuration. Safe mode next launch bypasses BepInEx entirely and starts the
native host with native mods paused. To update the game-local runtime, restore
native mode, install/update KeeperLoader, then enable compatibility again.
The same Enable / repair action refreshes only the owned official core, native
host, licences and notices from the verified bundled payload. Documentation
changes and rebuilt host DLLs no longer require uninstalling compatibility.
Installed plugins, disabled status, configuration and native mods are preserved.
Unknown runtime DLLs, unregistered plugins and patchers are still rejected.
Refresh is staged and rolls back if replacing a component fails.
To remove compatibility, uninstall managed external plugins first; the owned
BepInEx directory is archived in the game folder, not permanently deleted.

## Boundaries

- DLLs are read with Mono.Cecil by a precompiled, bounded metadata inspector.
  No DLL code, attribute constructors or plugin lifecycle methods run during
  installation. There is no player-side compiler or runtime downloader.
- One existing Doorstop bootstrap dispatches to the official preloader before
  game assemblies load. The official chainloader starts the Native Host adapter.
- Each external ZIP must contain exactly one concrete BepInEx 5 plugin.
  Other DLLs must be managed private dependencies with unique assembly names.
- ZIPs may contain loose plugin files or BepInEx/plugins/... with known root
  README/licence/changelog text files. Root documentation is retained in a
  manager-owned package documentation folder; plugin DLL/resource paths stay
  unchanged. Windows backslash directory entries are recognised even when the
  ZIP omits directory attributes. Runtime/core,
  config, patchers, game assemblies, shared Harmony/MonoMod assemblies, scripts,
  nested archives and mixed native/external packages are rejected.
- Process filters, GUIDs, versions, hard dependencies, incompatibilities,
  duplicate assemblies and dependency cycles are checked. The official
  chainloader additionally enforces its own loading rules.
- Absence of a process filter does not establish game compatibility. The user
  must confirm the publisher's target. Mods from GK1 cannot be assumed to run
  in GK2 or other games.
- Existing unowned BepInEx installations are not adopted, overwritten or removed.
- Plugins run in-process with the game's privileges. They are NOT sandboxed.
  Metadata checks/hashes do not establish that arbitrary mod code is safe and
  cannot rule out gameplay patch conflicts or prevent a plugin crashing the game.
- Only plugin-declared dependencies are checked ahead of time; arbitrary
  assembly references and patch compatibility still require runtime testing.
- No zero-detection VirusTotal result is promised.

## Upstream identity and licences

Unmodified official release: BepInEx 5.4.23.5 (MIT, copyright 2018 Bepis).
Release/source: https://github.com/BepInEx/BepInEx/releases/tag/v5.4.23.5
Pinned Windows x64 ZIP SHA-256:
82f9878551030f54657792c0740d9d51a09500eeae1fba21106b0c441e6732c4

The original ZIP is retained under upstream/. The release-tag licence, NOT
the different licence on the current BepInEx 6 development branch, applies.

Required upstream libraries: HarmonyX 2.9.0 (MIT), MonoMod 22.1.29.1 (MIT),
Mono.Cecil 0.10.4 (MIT), UnityDoorstop 4.5.0 (LGPL-2.1, existing distribution
notices). Licence texts are retained under licenses/ in the compatibility
payload. Source links:
https://github.com/BepInEx/HarmonyX/tree/v2.9.0
https://github.com/MonoMod/MonoMod/tree/v22.01.29.01
https://github.com/jbevain/cecil/tree/0.10.4
https://github.com/NeighTools/UnityDoorstop/tree/v4.5.0

KeeperLoader and its new adapter/inspector remain under the repository's
LGPL-2.1-or-later licence. No upstream BepInEx source is copied or modified.
