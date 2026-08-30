KeeperLoader Runtime for Graveyard Keeper
=========================================

This is the executable-free Nexus distribution. It contains only precompiled
managed KeeperLoader DLLs, the configuration file, and the official unmodified
UnityDoorstop x64 proxy DLL.

Manual installation
-------------------
1. Close Graveyard Keeper.
2. Open the Graveyard Keeper installation folder in Steam:
   Library > Graveyard Keeper > Properties > Installed Files > Browse.
3. Extract every file in this ZIP directly beside "Graveyard Keeper.exe".
4. Start the game normally through Steam.

Updating
--------
Extract a newer runtime ZIP to the same folder and replace the existing
KeeperLoader core DLLs, winhttp.dll, and doorstop_config.ini. Installed mods,
configuration, state, and saves are not included in this package and are not
replaced.

Mod management
--------------
Existing KeeperLoader mod ZIPs remain compatible. The optional KeeperLoader
Manager from the official GitHub release provides validated mod installation,
updates, enable/disable, rollback, and removal.

Removal
-------
Use the optional Manager's removal action when possible. For a manual runtime-only
installation, close the game, remove winhttp.dll and doorstop_config.ini only if
they came from this package, then remove the KeeperLoader folder. Game saves are
stored elsewhere and are not part of KeeperLoader.

Security
--------
Do not disable antivirus protection or create exclusions. SHA256SUMS.txt records
the exact hash of every distributed file. Source and reproducible Windows build
workflows are available in the official public GitHub repository.
