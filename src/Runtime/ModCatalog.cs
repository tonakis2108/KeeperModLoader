using System;
using System.Collections.Generic;
using System.IO;
using System.Reflection;
using KeeperLoader.API;

namespace KeeperLoader.Runtime
{
    internal sealed class ModCatalog
    {
        private readonly string _loaderDirectory;
        private readonly FileLogger _coreLog;
        private readonly List<LoadedMod> _loaded = new List<LoadedMod>();

        public ModCatalog(string loaderDirectory, FileLogger coreLog)
        {
            _loaderDirectory = loaderDirectory;
            _coreLog = coreLog;
        }

        public void DiscoverAndLoad()
        {
            string modsDirectory = Path.Combine(_loaderDirectory, "mods");
            Directory.CreateDirectory(modsDirectory);
            List<string> dllList = new List<string>();
            string[] modDirectories = Directory.GetDirectories(modsDirectory, "*", SearchOption.TopDirectoryOnly);
            Array.Sort(modDirectories, StringComparer.OrdinalIgnoreCase);
            for (int i = 0; i < modDirectories.Length; i++)
            {
                string name = Path.GetFileName(modDirectories[i]);
                if (name.StartsWith(".", StringComparison.Ordinal)) continue;
                if (File.Exists(Path.Combine(modDirectories[i], "keeperloader.disabled")))
                {
                    _coreLog.Info("Skipped disabled mod '" + name + "'.");
                    continue;
                }
                string entryMode;
                if (!IsActivatedForCurrentGame(modDirectories[i], out entryMode)) continue;
                if (entryMode.Equals("external-unity-plugin", StringComparison.OrdinalIgnoreCase))
                {
                    _coreLog.Warning("Skipped inactive legacy external package '" + name +
                        "'. External plugin support was removed in KeeperLoader 0.6.2; uninstall it through the manager.");
                    continue;
                }
                if (!entryMode.Equals("native", StringComparison.OrdinalIgnoreCase))
                {
                    _coreLog.Error("Skipped mod folder '" + name + "': unsupported entry mode '" + entryMode + "'.");
                    continue;
                }
                dllList.AddRange(Directory.GetFiles(modDirectories[i], "*.dll", SearchOption.AllDirectories));
            }
            string[] dlls = dllList.ToArray();
            Array.Sort(dlls, StringComparer.OrdinalIgnoreCase);
            List<ModCandidate> candidates = new List<ModCandidate>();
            Dictionary<string, ModCandidate> byId = new Dictionary<string, ModCandidate>(StringComparer.OrdinalIgnoreCase);

            for (int i = 0; i < dlls.Length; i++) DiscoverAssembly(dlls[i], candidates, byId);
            LoadInDependencyOrder(candidates, byId);
            _coreLog.Info("Loaded " + _loaded.Count + " native mod(s). Discovery found " +
                candidates.Count + " candidate(s).");
        }

        private bool IsActivatedForCurrentGame(string modDirectory, out string entryMode)
        {
            entryMode = "native";
            string marker = Path.Combine(modDirectory, "keeperloader.activation");
            if (!File.Exists(marker))
            {
                _coreLog.Warning("Skipped unmanaged mod folder '" + Path.GetFileName(modDirectory) +
                    "'. Reinstall it through KeeperLoader Manager.");
                return false;
            }
            try
            {
                Dictionary<string, string> values = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
                string[] lines = File.ReadAllLines(marker);
                for (int i = 0; i < lines.Length; i++)
                {
                    int equals = lines[i].IndexOf('=');
                    if (equals <= 0) continue;
                    values[lines[i].Substring(0, equals).Trim()] = lines[i].Substring(equals + 1).Trim();
                }
                string gameId;
                string backend;
                string compatibility;
                string modId;
                string currentGameId = Environment.GetEnvironmentVariable("KEEPERLOADER_GAME_ID") ?? "";
                if (!values.TryGetValue("game_id", out gameId) ||
                    !values.TryGetValue("backend", out backend) ||
                    !values.TryGetValue("compatibility", out compatibility) ||
                    !values.TryGetValue("mod_id", out modId) ||
                    !gameId.Equals(currentGameId, StringComparison.OrdinalIgnoreCase) ||
                    !backend.Equals("Mono", StringComparison.OrdinalIgnoreCase) ||
                    !compatibility.Equals("explicit-game-id", StringComparison.OrdinalIgnoreCase) ||
                    !modId.Equals(Path.GetFileName(modDirectory), StringComparison.OrdinalIgnoreCase))
                {
                    _coreLog.Error("Skipped mod folder '" + Path.GetFileName(modDirectory) +
                        "': activation record does not match this game.");
                    return false;
                }
                string declaredMode;
                if (values.TryGetValue("entry_mode", out declaredMode) && !string.IsNullOrEmpty(declaredMode))
                    entryMode = declaredMode;
                return true;
            }
            catch (Exception exception)
            {
                _coreLog.Error("Skipped mod folder '" + Path.GetFileName(modDirectory) +
                    "': activation record could not be read.", exception);
                return false;
            }
        }

        private void DiscoverAssembly(string path, List<ModCandidate> candidates,
            Dictionary<string, ModCandidate> byId)
        {
            try
            {
                Assembly assembly = Assembly.LoadFrom(path);
                Type[] types;
                try { types = assembly.GetTypes(); }
                catch (ReflectionTypeLoadException exception) { types = exception.Types; }
                for (int i = 0; i < types.Length; i++)
                {
                    Type type = types[i];
                    if (type == null || type.IsAbstract || !typeof(IKeeperMod).IsAssignableFrom(type)) continue;
                    object[] attributes = type.GetCustomAttributes(typeof(KeeperModAttribute), false);
                    if (attributes.Length != 1) continue;
                    KeeperModAttribute metadata = (KeeperModAttribute)attributes[0];
                    if (!IsValidId(metadata.Id))
                    {
                        _coreLog.Error("Rejected mod type " + type.FullName + ": invalid id '" + metadata.Id + "'.");
                        continue;
                    }
                    string expectedId = GetContainingModId(path);
                    if (!metadata.Id.Equals(expectedId, StringComparison.OrdinalIgnoreCase))
                    {
                        _coreLog.Error("Rejected mod type " + type.FullName + ": assembly id '" +
                            metadata.Id + "' does not match activated package id '" + expectedId + "'.");
                        continue;
                    }
                    if (byId.ContainsKey(metadata.Id))
                    {
                        _coreLog.Error("Rejected duplicate mod id '" + metadata.Id + "' from " + path + ".");
                        continue;
                    }
                    ModCandidate candidate = new ModCandidate(type, metadata, path);
                    object[] dependencyAttributes = type.GetCustomAttributes(typeof(KeeperDependencyAttribute), false);
                    for (int d = 0; d < dependencyAttributes.Length; d++)
                        candidate.Dependencies.Add((KeeperDependencyAttribute)dependencyAttributes[d]);
                    candidates.Add(candidate);
                    byId.Add(metadata.Id, candidate);
                }
            }
            catch (Exception exception)
            {
                _coreLog.Error("Could not inspect mod assembly " + path + ".", exception);
            }
        }

        private string GetContainingModId(string assemblyPath)
        {
            string modsRoot = Path.GetFullPath(Path.Combine(_loaderDirectory, "mods")) +
                Path.DirectorySeparatorChar;
            string fullPath = Path.GetFullPath(assemblyPath);
            if (!fullPath.StartsWith(modsRoot, StringComparison.OrdinalIgnoreCase)) return "";
            string relative = fullPath.Substring(modsRoot.Length);
            int separator = relative.IndexOf(Path.DirectorySeparatorChar);
            return separator <= 0 ? "" : relative.Substring(0, separator);
        }

        private void LoadInDependencyOrder(List<ModCandidate> candidates,
            Dictionary<string, ModCandidate> byId)
        {
            List<ModCandidate> pending = new List<ModCandidate>(candidates);
            Dictionary<string, LoadedMod> loadedById = new Dictionary<string, LoadedMod>(StringComparer.OrdinalIgnoreCase);
            bool progress = true;
            while (pending.Count > 0 && progress)
            {
                progress = false;
                for (int i = pending.Count - 1; i >= 0; i--)
                {
                    ModCandidate candidate = pending[i];
                    string failure;
                    DependencyState state = CheckDependencies(candidate, byId, loadedById, out failure);
                    if (state == DependencyState.Waiting) continue;
                    pending.RemoveAt(i);
                    progress = true;
                    if (state == DependencyState.Failed)
                    {
                        _coreLog.Error("Skipped " + candidate.Metadata.Id + ": " + failure);
                        continue;
                    }
                    LoadedMod loaded = LoadCandidate(candidate);
                    if (loaded != null)
                    {
                        _loaded.Add(loaded);
                        loadedById.Add(candidate.Metadata.Id, loaded);
                    }
                }
            }
            for (int i = 0; i < pending.Count; i++)
                _coreLog.Error("Skipped " + pending[i].Metadata.Id + ": circular or unresolved dependency chain.");
        }

        private DependencyState CheckDependencies(ModCandidate candidate,
            Dictionary<string, ModCandidate> candidates, Dictionary<string, LoadedMod> loaded,
            out string failure)
        {
            failure = null;
            for (int i = 0; i < candidate.Dependencies.Count; i++)
            {
                KeeperDependencyAttribute dependency = candidate.Dependencies[i];
                ModCandidate dependencyCandidate;
                if (!candidates.TryGetValue(dependency.Id, out dependencyCandidate))
                {
                    if (dependency.Optional) continue;
                    failure = "missing dependency " + dependency.Id;
                    return DependencyState.Failed;
                }
                LoadedMod loadedDependency;
                if (!loaded.TryGetValue(dependency.Id, out loadedDependency)) return DependencyState.Waiting;
                Version actual = ParseVersion(loadedDependency.Metadata.Version);
                Version required = ParseVersion(dependency.MinimumVersion);
                if (actual.CompareTo(required) < 0)
                {
                    failure = dependency.Id + " " + dependency.MinimumVersion + "+ is required; found " +
                              loadedDependency.Metadata.Version;
                    return DependencyState.Failed;
                }
            }
            return DependencyState.Ready;
        }

        private LoadedMod LoadCandidate(ModCandidate candidate)
        {
            FileLogger log = new FileLogger(Path.Combine(_loaderDirectory, "logs", "latest.log"), candidate.Metadata.Id);
            try
            {
                IKeeperMod instance = (IKeeperMod)Activator.CreateInstance(candidate.Type);
                string modDirectory = Path.GetDirectoryName(candidate.AssemblyPath);
                string configDirectory = Path.Combine(_loaderDirectory, "config");
                string stateDirectory = Path.Combine(_loaderDirectory, "state", candidate.Metadata.Id);
                Directory.CreateDirectory(configDirectory);
                Directory.CreateDirectory(stateDirectory);
                KeeperConfig config = new KeeperConfig(Path.Combine(configDirectory, candidate.Metadata.Id + ".cfg"));
                string gameDirectory = Environment.GetEnvironmentVariable("KEEPERLOADER_GAME_DIR");
                string gameExecutable = Environment.GetEnvironmentVariable("KEEPERLOADER_GAME_EXE");
                string gameId = Environment.GetEnvironmentVariable("KEEPERLOADER_GAME_ID");
                string managedDirectory = Environment.GetEnvironmentVariable("DOORSTOP_MANAGED_FOLDER_DIR");
                KeeperContext context = new KeeperContext(gameDirectory, gameExecutable, gameId,
                    managedDirectory, _loaderDirectory, modDirectory, configDirectory, stateDirectory, log, config);
                instance.OnLoad(context);
                log.Info("Loaded " + candidate.Metadata.Name + " " + candidate.Metadata.Version + ".");
                return new LoadedMod(candidate.Metadata, instance, log);
            }
            catch (Exception exception)
            {
                log.Error("Load failed; the mod was isolated and the game will continue.", Unwrap(exception));
                return null;
            }
        }

        public void Update()
        {
            for (int i = 0; i < _loaded.Count; i++) InvokeSafely(_loaded[i], false);
        }

        public void DrawGUI()
        {
            for (int i = 0; i < _loaded.Count; i++) InvokeSafely(_loaded[i], true);
        }

        private void InvokeSafely(LoadedMod mod, bool gui)
        {
            if (mod.Disabled) return;
            try
            {
                if (gui) mod.Instance.OnGUI(); else mod.Instance.OnUpdate();
                mod.ConsecutiveErrors = 0;
            }
            catch (Exception exception)
            {
                mod.ConsecutiveErrors++;
                mod.Log.Error((gui ? "OnGUI" : "OnUpdate") + " failed (" + mod.ConsecutiveErrors + "/3).", exception);
                if (mod.ConsecutiveErrors >= 3)
                {
                    mod.Disabled = true;
                    mod.Log.Error("Mod disabled for the remainder of this session after repeated errors.");
                }
            }
        }

        public void UnloadAll()
        {
            for (int i = _loaded.Count - 1; i >= 0; i--)
            {
                try { _loaded[i].Instance.OnUnload(); }
                catch (Exception exception) { _loaded[i].Log.Error("OnUnload failed.", exception); }
            }
            _loaded.Clear();
        }

        private static bool IsValidId(string id)
        {
            if (string.IsNullOrEmpty(id) || id.Length > 80) return false;
            for (int i = 0; i < id.Length; i++)
            {
                char c = id[i];
                if (!(char.IsLetterOrDigit(c) || c == '.' || c == '-' || c == '_')) return false;
            }
            return true;
        }

        private static Version ParseVersion(string value)
        {
            Version parsed;
            return Version.TryParse(value, out parsed) ? parsed : new Version(0, 0, 0);
        }

        private static Exception Unwrap(Exception exception)
        {
            TargetInvocationException invocation = exception as TargetInvocationException;
            return invocation != null && invocation.InnerException != null ? invocation.InnerException : exception;
        }

        private enum DependencyState { Waiting, Ready, Failed }

        private sealed class ModCandidate
        {
            public ModCandidate(Type type, KeeperModAttribute metadata, string path)
            {
                Type = type;
                Metadata = metadata;
                AssemblyPath = path;
                Dependencies = new List<KeeperDependencyAttribute>();
            }
            public Type Type;
            public KeeperModAttribute Metadata;
            public string AssemblyPath;
            public List<KeeperDependencyAttribute> Dependencies;
        }

        private sealed class LoadedMod
        {
            public LoadedMod(KeeperModAttribute metadata, IKeeperMod instance, FileLogger log)
            {
                Metadata = metadata;
                Instance = instance;
                Log = log;
            }
            public KeeperModAttribute Metadata;
            public IKeeperMod Instance;
            public FileLogger Log;
            public int ConsecutiveErrors;
            public bool Disabled;
        }
    }
}
