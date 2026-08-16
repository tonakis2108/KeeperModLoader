# Developing KeeperLoader mods

## Minimal mod

Reference these assemblies:

- `KeeperLoader\core\KeeperLoader.API.dll`
- The Unity assemblies you use from the selected game's `<Game>_Data\Managed` folder
- Game assemblies only when the mod intentionally targets that game

Then implement the public contract:

```csharp
using KeeperLoader.API;

[KeeperMod("example.hello", "Hello Keeper", "1.0.0")]
public sealed class HelloKeeper : KeeperMod
{
    private KeeperContext _context;

    public override void OnLoad(KeeperContext context)
    {
        _context = context;
        context.Log.Info("Hello from KeeperLoader.");
    }

    public override void OnUpdate()
    {
        // Called on Unity's main thread every frame.
    }

    public override void OnGUI()
    {
        // Called from Unity's IMGUI event loop.
    }

    public override void OnUnload()
    {
        // Destroy created objects and release resources here.
    }
}
```

Place the compiled DLL in its own directory:

`KeeperLoader\mods\example.hello\Example.Hello.dll`

For distribution, do not ask players to copy that directory manually. Put the compiled DLL and private dependencies in a clean folder. Open **Manage mods...** in `KeeperLoader-Manager.exe`, choose **Build Mod ZIP...**, and enter the package metadata. The manager creates the manifest and hashes each payload file.

Players install the resulting ZIP through **Install Mod ZIP...**. KeeperLoader verifies its manifest and every file hash, stages installation, and backs up an older version with the same mod ID.

Every package must list explicit normalized game IDs shown by the manager. Wildcard compatibility is rejected, including for UI-only mods. Multiple IDs are entered as a comma-separated list.

## Metadata rules

`KeeperModAttribute` requires:

- `id`: unique, stable, 80 characters or fewer; letters, digits, `.`, `-`, and `_` only.
- `name`: user-facing name.
- `version`: a numeric `System.Version` string such as `1.2.0`.

Each package must expose one concrete `IKeeperMod` whose `KeeperModAttribute` ID exactly matches the package manifest ID. Helper assemblies may be included, but additional independently identified mods belong in separate packages.

## Dependencies

Declare required dependencies:

```csharp
[KeeperMod("example.addon", "Example Add-on", "1.0.0")]
[KeeperDependency("example.core", "2.1.0", false)]
public sealed class ExampleAddon : KeeperMod
{
}
```

The third argument marks the dependency as optional when `true`. Required dependencies are version-checked and loaded before the dependent mod. Missing dependencies and cycles are reported without stopping the game.

## Context

`KeeperContext` provides:

- `GameDirectory`
- `GameExecutablePath`
- `GameId`
- `ManagedDirectory`
- `LoaderDirectory`
- `ModDirectory`
- `ConfigDirectory`
- `StateDirectory`
- `Log`
- `Config`

Use `StateDirectory` for mod-owned persistent state. Never modify game saves unless that is the explicit purpose of the mod and users are clearly warned.

## Cross-game compatibility

The lifecycle, logging, configuration, state, and common Unity APIs are portable across supported Unity Mono games. Game-owned classes are not. A reference to `Assembly-CSharp.dll`, a reflected player type, a scene name, or a save schema normally makes the mod game-specific.

Keep universal code behind KeeperLoader and Unity API boundaries. Place game adapters in separate assemblies or packages, and declare every supported game ID honestly. The installer enforces that declaration before activation.

## Configuration

Configuration is a sorted UTF-8 key/value file. Reading a missing value writes its default:

```csharp
bool enabled = context.Config.GetBool("enabled", true);
int size = context.Config.GetInt("size", 240, 100, 500);
float scale = context.Config.GetFloat("scale", 1.0f, 0.5f, 3.0f);
context.Config.Set("enabled", false);
```

## Error isolation

- An exception from `OnLoad` prevents only that mod from loading.
- Exceptions from `OnUpdate` or `OnGUI` are logged.
- Three consecutive callback failures disable that mod for the rest of the session.
- An exception from `OnUnload` is logged while other mods continue shutting down.

Mods are trusted code running inside the game process. KeeperLoader isolates ordinary managed failures; it cannot sandbox malicious code, native crashes, corrupted memory, or unsafe patching libraries.

## Cleanup

`OnUnload` should destroy every GameObject, texture, camera, material, and unmanaged resource created by the mod. Static event subscriptions should also be removed. A clean lifecycle is mandatory even though live assembly unloading is not supported.
