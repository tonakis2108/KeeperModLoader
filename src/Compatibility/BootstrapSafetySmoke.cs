using System;
using System.IO;
using System.Reflection;

// CI-only: validates the bootstrap gate without loading any external runtime.
internal static class BootstrapSafetySmoke
{
    private static MethodInfo _verify;
    private static void CheckRejected(string path)
    {
        File.WriteAllText(path, "unexpected DLL");
        bool rejected = false;
        try { _verify.Invoke(null, null); }
        catch (TargetInvocationException e)
        {
            if (!(e.InnerException is InvalidDataException)) throw;
            rejected = true;
        }
        finally { File.Delete(path); }
        if (!rejected) throw new Exception("Bootstrap accepted an unregistered DLL: " + path);
    }

    private static int Main(string[] args)
    {
        Type entry = Assembly.LoadFrom(args[0]).GetType("Doorstop.Entrypoint", true);
        BindingFlags hidden = BindingFlags.NonPublic | BindingFlags.Static;
        string game = Path.GetFullPath(args[1]);
        string loader = Path.Combine(game, "KeeperLoader");
        Directory.CreateDirectory(Path.Combine(loader, "logs"));
        Directory.CreateDirectory(Path.Combine(loader, "state"));
        entry.GetField("_gameDirectory", hidden).SetValue(null, game);
        entry.GetField("_loaderDirectory", hidden).SetValue(null, loader);
        entry.GetField("_logPath", hidden).SetValue(null, Path.Combine(loader, "logs", "bootstrap.log"));
        _verify = entry.GetMethod("VerifyCompatibilityRuntime", hidden);
        _verify.Invoke(null, null);
        string root = Path.Combine(game, "BepInEx");
        CheckRejected(Path.Combine(root, "core", "Injected.dll"));
        CheckRejected(Path.Combine(root, "plugins", "KeeperLoaderManaged", "Loose.dll"));
        CheckRejected(Path.Combine(root, "plugins", "KeeperLoaderManaged", "keeperloader.nativehost", "Injected.dll"));
        string safeMode = Path.Combine(loader, "state", "safe-mode.next");
        File.WriteAllText(safeMode, "explicitly requested");
        entry.GetMethod("PrepareEnvironment", hidden).Invoke(null, null);
        if (File.Exists(safeMode) || Environment.GetEnvironmentVariable("KEEPERLOADER_SAFE_MODE") != "1")
            throw new Exception("User-selected safe mode was not consumed for one launch.");
        entry.GetMethod("PrepareEnvironment", hidden).Invoke(null, null);
        if (Environment.GetEnvironmentVariable("KEEPERLOADER_SAFE_MODE") != null)
            throw new Exception("Safe mode incorrectly persisted for a second launch.");
        Console.WriteLine("Bootstrap ownership, DLL boundaries and one-launch safe-mode checks passed.");
        return 0;
    }
}
