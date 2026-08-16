using System;
using System.Collections.Generic;
using System.IO;
using System.Reflection;
using UnityEngine;

namespace KeeperLoader.Runtime
{
    internal sealed class ExternalPluginCatalog
    {
        private readonly string _loaderDirectory;
        private readonly FileLogger _coreLog;
        private readonly List<GameObject> _hosts = new List<GameObject>();
        private readonly HashSet<string> _loadedIds = new HashSet<string>(StringComparer.OrdinalIgnoreCase);

        public ExternalPluginCatalog(string loaderDirectory, FileLogger coreLog)
        {
            _loaderDirectory = loaderDirectory;
            _coreLog = coreLog;
        }

        public int DiscoverAndLoad(string modDirectory)
        {
            string packageId = Path.GetFileName(modDirectory);
            string[] dlls = Directory.GetFiles(modDirectory, "*.dll", SearchOption.AllDirectories);
            Array.Sort(dlls, StringComparer.OrdinalIgnoreCase);
            int loaded = 0;
            List<string> failures = new List<string>();

            for (int i = 0; i < dlls.Length; i++)
            {
                try
                {
                    Assembly assembly = Assembly.LoadFrom(dlls[i]);
                    Type[] types;
                    try
                    {
                        types = assembly.GetTypes();
                    }
                    catch (ReflectionTypeLoadException exception)
                    {
                        types = exception.Types;
                        RecordLoaderFailures(exception, failures);
                    }
                    for (int t = 0; t < types.Length; t++)
                    {
                        string pluginId;
                        string pluginName;
                        string pluginVersion;
                        if (!TryDescribePlugin(types[t], out pluginId, out pluginName, out pluginVersion)) continue;
                        if (_loadedIds.Contains(pluginId))
                        {
                            failures.Add("duplicate plugin id " + pluginId);
                            continue;
                        }
                        if (TryAttach(types[t], pluginId, pluginName, pluginVersion, failures))
                        {
                            _loadedIds.Add(pluginId);
                            loaded++;
                        }
                    }
                }
                catch (Exception exception)
                {
                    failures.Add(Path.GetFileName(dlls[i]) + ": " + OneLine(Unwrap(exception).Message));
                }
            }

            if (loaded > 0)
            {
                string message = "Attached " + loaded + " external Unity plugin component(s).";
                WriteStatus(packageId, "loaded", message);
                _coreLog.Info(packageId + ": " + message);
            }
            else
            {
                string message = failures.Count == 0
                    ? "No compatible external Unity plugin entry point was found."
                    : "No component could be attached. " + failures[0];
                WriteStatus(packageId, "incompatible", message);
                _coreLog.Warning(packageId + ": " + message);
            }
            for (int i = 1; i < failures.Count && i < 8; i++)
                _coreLog.Warning(packageId + ": " + failures[i]);
            return loaded;
        }

        private bool TryAttach(Type type, string id, string name, string version, List<string> failures)
        {
            GameObject host = null;
            try
            {
                host = new GameObject("KeeperLoader External Plugin " + id);
                host.hideFlags = HideFlags.HideAndDontSave;
                UnityEngine.Object.DontDestroyOnLoad(host);
                Component component = host.AddComponent(type);
                if (component == null) throw new InvalidOperationException("Unity did not create the plugin component");
                _hosts.Add(host);
                _coreLog.Info("Attached external plugin " + name + " " + version + " (" + id + ").");
                return true;
            }
            catch (Exception exception)
            {
                if (host != null) UnityEngine.Object.Destroy(host);
                failures.Add(name + ": " + OneLine(Unwrap(exception).Message));
                return false;
            }
        }

        private static bool TryDescribePlugin(Type type, out string id, out string name, out string version)
        {
            id = null;
            name = null;
            version = null;
            if (type == null || type.IsAbstract || !typeof(MonoBehaviour).IsAssignableFrom(type)) return false;

            Type externalBase = null;
            Type current = type.BaseType;
            while (current != null && current != typeof(MonoBehaviour))
            {
                if (current.Assembly != type.Assembly && current.Assembly != typeof(MonoBehaviour).Assembly)
                    externalBase = current;
                current = current.BaseType;
            }
            if (current != typeof(MonoBehaviour) || externalBase == null) return false;

            IList<CustomAttributeData> attributes;
            try { attributes = CustomAttributeData.GetCustomAttributes(type); }
            catch { return false; }
            for (int i = 0; i < attributes.Count; i++)
            {
                CustomAttributeData attribute = attributes[i];
                if (attribute.Constructor == null || attribute.Constructor.DeclaringType == null ||
                    attribute.Constructor.DeclaringType.Assembly == type.Assembly ||
                    attribute.ConstructorArguments.Count < 3) continue;
                string first = attribute.ConstructorArguments[0].Value as string;
                string second = attribute.ConstructorArguments[1].Value as string;
                string third = attribute.ConstructorArguments[2].Value as string;
                Version parsed;
                if (!IsValidId(first) || string.IsNullOrEmpty(second) || !Version.TryParse(third, out parsed)) continue;
                id = first;
                name = second;
                version = third;
                return true;
            }
            return false;
        }

        private static void RecordLoaderFailures(ReflectionTypeLoadException exception, List<string> failures)
        {
            if (exception.LoaderExceptions == null) return;
            for (int i = 0; i < exception.LoaderExceptions.Length && i < 8; i++)
            {
                if (exception.LoaderExceptions[i] != null)
                    failures.Add(OneLine(exception.LoaderExceptions[i].Message));
            }
        }

        private void WriteStatus(string packageId, string status, string message)
        {
            try
            {
                string directory = Path.Combine(_loaderDirectory, "state", packageId);
                Directory.CreateDirectory(directory);
                string record = "status=" + status + Environment.NewLine +
                                "message=" + OneLine(message) + Environment.NewLine +
                                "updated_at_utc=" + DateTime.UtcNow.ToString("o") + Environment.NewLine;
                File.WriteAllText(Path.Combine(directory, "external-plugin.status"), record);
            }
            catch (Exception exception)
            {
                _coreLog.Warning("Could not write external plugin status for " + packageId + ": " + exception.Message);
            }
        }

        public void UnloadAll()
        {
            for (int i = _hosts.Count - 1; i >= 0; i--)
            {
                if (_hosts[i] != null) UnityEngine.Object.Destroy(_hosts[i]);
            }
            _hosts.Clear();
            _loadedIds.Clear();
        }

        private static bool IsValidId(string value)
        {
            if (string.IsNullOrEmpty(value) || value.Length > 160) return false;
            for (int i = 0; i < value.Length; i++)
            {
                char c = value[i];
                if (!(char.IsLetterOrDigit(c) || c == '.' || c == '-' || c == '_')) return false;
            }
            return true;
        }

        private static string OneLine(string value)
        {
            if (string.IsNullOrEmpty(value)) return "unknown compatibility error";
            return value.Replace('\r', ' ').Replace('\n', ' ').Trim();
        }

        private static Exception Unwrap(Exception exception)
        {
            TargetInvocationException invocation = exception as TargetInvocationException;
            return invocation != null && invocation.InnerException != null ? invocation.InnerException : exception;
        }
    }
}
