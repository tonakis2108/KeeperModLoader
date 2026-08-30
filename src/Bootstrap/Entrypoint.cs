using System;
using System.Collections.Generic;
using System.IO;
using System.Reflection;

namespace Doorstop
{
    public static class Entrypoint
    {
        private static readonly object Sync = new object();
        private static string _gameDirectory;
        private static string _loaderDirectory;
        private static string _logPath;
        private static bool _runtimeStarted;
        private static Dictionary<string, string> _assemblyIndex;
        private static HashSet<string> _startupAssemblies;

        public static void Start()
        {
            try
            {
                string processPath = Environment.GetEnvironmentVariable("DOORSTOP_PROCESS_PATH");
                _gameDirectory = string.IsNullOrEmpty(processPath)
                    ? Environment.CurrentDirectory
                    : Path.GetDirectoryName(Path.GetFullPath(processPath));
                _loaderDirectory = Path.Combine(_gameDirectory, "KeeperLoader");
                Directory.CreateDirectory(Path.Combine(_loaderDirectory, "logs"));
                Directory.CreateDirectory(Path.Combine(_loaderDirectory, "state"));
                Directory.CreateDirectory(Path.Combine(_loaderDirectory, "mods"));
                Directory.CreateDirectory(Path.Combine(_loaderDirectory, "config"));
                _logPath = Path.Combine(_loaderDirectory, "logs", "latest.log");
                RotateLog();
                Log("KeeperLoader bootstrap 0.7.4 starting.");
                Log("Process path: " + processPath);

                PrepareEnvironment();
                Log("KeeperLoader environment initialized.");
                BuildStartupAssemblySet();
                Log("Startup assembly triggers initialized.");
                BuildAssemblyIndex();
                Log("Loader assembly index initialized.");
                AppDomain.CurrentDomain.AssemblyResolve += ResolveAssembly;
                AppDomain.CurrentDomain.AssemblyLoad += OnAssemblyLoad;

                Assembly[] loaded = AppDomain.CurrentDomain.GetAssemblies();
                for (int i = 0; i < loaded.Length; i++)
                {
                    if (_startupAssemblies.Contains(loaded[i].GetName().Name))
                    {
                        StartRuntime();
                        break;
                    }
                }
            }
            catch (Exception exception)
            {
                Log("FATAL bootstrap error: " + exception);
            }
        }

        private static void PrepareEnvironment()
        {
            string stateDirectory = Path.Combine(_loaderDirectory, "state");
            string safeModeRequest = Path.Combine(stateDirectory, "safe-mode.next");
            Environment.SetEnvironmentVariable("KEEPERLOADER_SAFE_MODE", null);
            if (File.Exists(safeModeRequest))
            {
                try
                {
                    File.Delete(safeModeRequest);
                    Environment.SetEnvironmentVariable("KEEPERLOADER_SAFE_MODE", "1");
                    Log("User-selected safe mode is enabled for this launch only.");
                }
                catch (Exception exception)
                {
                    Log("Could not consume the safe-mode request; continuing with mods enabled: " +
                        exception.Message);
                }
            }
            Environment.SetEnvironmentVariable("KEEPERLOADER_GAME_DIR", _gameDirectory);
            Environment.SetEnvironmentVariable("KEEPERLOADER_DIR", _loaderDirectory);
            string processPath = Environment.GetEnvironmentVariable("DOORSTOP_PROCESS_PATH");
            if (!string.IsNullOrEmpty(processPath))
            {
                string gameName = Path.GetFileNameWithoutExtension(processPath);
                Environment.SetEnvironmentVariable("KEEPERLOADER_GAME_EXE", Path.GetFullPath(processPath));
                Environment.SetEnvironmentVariable("KEEPERLOADER_GAME_ID", NormalizeGameId(gameName));
            }
        }

        private static void OnAssemblyLoad(object sender, AssemblyLoadEventArgs args)
        {
            if (_startupAssemblies.Contains(args.LoadedAssembly.GetName().Name)) StartRuntime();
        }

        private static void BuildStartupAssemblySet()
        {
            _startupAssemblies = new HashSet<string>(StringComparer.OrdinalIgnoreCase);
            string managedDirectory = Environment.GetEnvironmentVariable("DOORSTOP_MANAGED_FOLDER_DIR");
            if (string.IsNullOrEmpty(managedDirectory) || !Directory.Exists(managedDirectory))
            {
                Log("FATAL: Doorstop did not provide a valid Unity Mono Managed directory.");
                return;
            }

            string assemblyCSharp = Path.Combine(managedDirectory, "Assembly-CSharp.dll");
            if (File.Exists(assemblyCSharp))
            {
                _startupAssemblies.Add("Assembly-CSharp");
                return;
            }

            string[] files = Directory.GetFiles(managedDirectory, "*.dll", SearchOption.TopDirectoryOnly);
            Array.Sort(files, StringComparer.OrdinalIgnoreCase);
            for (int i = 0; i < files.Length; i++)
            {
                string name = Path.GetFileNameWithoutExtension(files[i]);
                if (!IsFrameworkAssembly(name)) _startupAssemblies.Add(name);
            }
            Log("Runtime trigger candidates: " + _startupAssemblies.Count + ".");
        }

        private static bool IsFrameworkAssembly(string name)
        {
            return name.Equals("mscorlib", StringComparison.OrdinalIgnoreCase) ||
                   name.Equals("netstandard", StringComparison.OrdinalIgnoreCase) ||
                   name.StartsWith("System", StringComparison.OrdinalIgnoreCase) ||
                   name.StartsWith("Microsoft", StringComparison.OrdinalIgnoreCase) ||
                   name.StartsWith("Mono.", StringComparison.OrdinalIgnoreCase) ||
                   name.StartsWith("UnityEngine", StringComparison.OrdinalIgnoreCase) ||
                   name.StartsWith("Unity.", StringComparison.OrdinalIgnoreCase) ||
                   name.StartsWith("KeeperLoader", StringComparison.OrdinalIgnoreCase);
        }

        private static string NormalizeGameId(string value)
        {
            if (string.IsNullOrEmpty(value)) return "unknown-game";
            System.Text.StringBuilder result = new System.Text.StringBuilder();
            bool separator = false;
            for (int i = 0; i < value.Length; i++)
            {
                char c = char.ToLowerInvariant(value[i]);
                if (char.IsLetterOrDigit(c))
                {
                    if (separator && result.Length > 0) result.Append('-');
                    result.Append(c);
                    separator = false;
                }
                else separator = true;
            }
            return result.Length == 0 ? "unknown-game" : result.ToString();
        }

        private static void StartRuntime()
        {
            lock (Sync)
            {
                if (_runtimeStarted) return;
                _runtimeStarted = true;
            }

            try
            {
                string runtimePath = Path.Combine(_loaderDirectory, "core", "KeeperLoader.Runtime.dll");
                Assembly runtime = Assembly.LoadFrom(runtimePath);
                Type host = runtime.GetType("KeeperLoader.Runtime.RuntimeHost", true);
                MethodInfo attach = host.GetMethod("Attach", BindingFlags.Public | BindingFlags.Static);
                attach.Invoke(null, null);
                Log("Managed runtime attached.");
            }
            catch (Exception exception)
            {
                Log("FATAL runtime attach error: " + exception);
            }
        }

        private static void BuildAssemblyIndex()
        {
            _assemblyIndex = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
            IndexDirectory(Path.Combine(_loaderDirectory, "core"));
            if (!string.Equals(Environment.GetEnvironmentVariable("KEEPERLOADER_SAFE_MODE"), "1",
                StringComparison.Ordinal))
            {
                string modsDirectory = Path.Combine(_loaderDirectory, "mods");
                if (Directory.Exists(modsDirectory))
                {
                    string[] modDirectories = Directory.GetDirectories(modsDirectory, "*",
                        SearchOption.TopDirectoryOnly);
                    Array.Sort(modDirectories, StringComparer.OrdinalIgnoreCase);
                    for (int i = 0; i < modDirectories.Length; i++)
                    {
                        if (File.Exists(Path.Combine(modDirectories[i], "keeperloader.disabled"))) continue;
                        IndexDirectory(modDirectories[i]);
                    }
                }
            }
        }

        private static void IndexDirectory(string directory)
        {
            if (!Directory.Exists(directory)) return;
            string[] files = Directory.GetFiles(directory, "*.dll", SearchOption.AllDirectories);
            Array.Sort(files, StringComparer.OrdinalIgnoreCase);
            for (int i = 0; i < files.Length; i++)
            {
                try
                {
                    string name = AssemblyName.GetAssemblyName(files[i]).Name;
                    if (!_assemblyIndex.ContainsKey(name)) _assemblyIndex.Add(name, files[i]);
                }
                catch { }
            }
        }

        private static Assembly ResolveAssembly(object sender, ResolveEventArgs args)
        {
            try
            {
                string name = new AssemblyName(args.Name).Name;
                string path;
                if (_assemblyIndex.TryGetValue(name, out path)) return Assembly.LoadFrom(path);
            }
            catch (Exception exception)
            {
                Log("Assembly resolution error: " + exception.Message);
            }
            return null;
        }

        private static void RotateLog()
        {
            try
            {
                string previous = Path.Combine(Path.GetDirectoryName(_logPath), "previous.log");
                if (File.Exists(previous)) File.Delete(previous);
                if (File.Exists(_logPath)) File.Move(_logPath, previous);
            }
            catch { }
        }

        private static void Log(string message)
        {
            try
            {
                string line = DateTime.Now.ToString("HH:mm:ss.fff") + " [Bootstrap] " + message;
                File.AppendAllText(_logPath, line + Environment.NewLine);
                Console.WriteLine(line);
            }
            catch { }
        }
    }
}
