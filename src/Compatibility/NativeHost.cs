using System;
using System.IO;
using System.Reflection;
using BepInEx;

namespace KeeperLoader.Compatibility
{
    // Only this adapter depends on BepInEx. The native API remains unchanged.
    [BepInPlugin("keeperloader.nativehost", "KeeperLoader Native Host", "0.8.0")]
    public sealed class NativeHost : BaseUnityPlugin
    {
        private void Awake()
        {
            try
            {
                string root = Environment.GetEnvironmentVariable("KEEPERLOADER_DIR");
                if (string.IsNullOrEmpty(root)) throw new InvalidOperationException("KeeperLoader bootstrap was not initialized.");
                Assembly bootstrap = Assembly.LoadFrom(Path.Combine(root, "core", "KeeperLoader.Bootstrap.dll"));
                bootstrap.GetType("Doorstop.Entrypoint", true).GetMethod("StartNativeFromCompatibility",
                    BindingFlags.Public | BindingFlags.Static).Invoke(null, null);
                Logger.LogInfo("KeeperLoader native runtime requested; existing native packages and settings are unchanged.");
            }
            catch (Exception error)
            {
                Logger.LogError("KeeperLoader native host failed: " + error);
            }
        }
    }
}
