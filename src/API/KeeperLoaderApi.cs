using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;

namespace KeeperLoader.API
{
    [AttributeUsage(AttributeTargets.Class, AllowMultiple = false, Inherited = false)]
    public sealed class KeeperModAttribute : Attribute
    {
        public KeeperModAttribute(string id, string name, string version)
        {
            Id = id;
            Name = name;
            Version = version;
        }

        public string Id { get; private set; }
        public string Name { get; private set; }
        public string Version { get; private set; }
    }

    [AttributeUsage(AttributeTargets.Class, AllowMultiple = true, Inherited = false)]
    public sealed class KeeperDependencyAttribute : Attribute
    {
        public KeeperDependencyAttribute(string id) : this(id, "0.0.0", false) { }

        public KeeperDependencyAttribute(string id, string minimumVersion, bool optional)
        {
            Id = id;
            MinimumVersion = minimumVersion;
            Optional = optional;
        }

        public string Id { get; private set; }
        public string MinimumVersion { get; private set; }
        public bool Optional { get; private set; }
    }

    public interface IKeeperMod
    {
        void OnLoad(KeeperContext context);
        void OnUpdate();
        void OnGUI();
        void OnUnload();
    }

    public abstract class KeeperMod : IKeeperMod
    {
        public virtual void OnLoad(KeeperContext context) { }
        public virtual void OnUpdate() { }
        public virtual void OnGUI() { }
        public virtual void OnUnload() { }
    }

    public interface IKeeperLogger
    {
        void Info(string message);
        void Warning(string message);
        void Error(string message);
        void Error(string message, Exception exception);
    }

    public sealed class KeeperContext
    {
        public KeeperContext(string gameDirectory, string gameExecutablePath, string gameId,
            string managedDirectory, string loaderDirectory, string modDirectory,
            string configDirectory, string stateDirectory, IKeeperLogger logger, KeeperConfig config)
        {
            GameDirectory = gameDirectory;
            GameExecutablePath = gameExecutablePath;
            GameId = gameId;
            ManagedDirectory = managedDirectory;
            LoaderDirectory = loaderDirectory;
            ModDirectory = modDirectory;
            ConfigDirectory = configDirectory;
            StateDirectory = stateDirectory;
            Log = logger;
            Config = config;
        }

        public string GameDirectory { get; private set; }
        public string GameExecutablePath { get; private set; }
        public string GameId { get; private set; }
        public string ManagedDirectory { get; private set; }
        public string LoaderDirectory { get; private set; }
        public string ModDirectory { get; private set; }
        public string ConfigDirectory { get; private set; }
        public string StateDirectory { get; private set; }
        public IKeeperLogger Log { get; private set; }
        public KeeperConfig Config { get; private set; }
    }

    public sealed class KeeperConfig
    {
        private readonly string _path;
        private readonly object _sync = new object();
        private readonly Dictionary<string, string> _values =
            new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);

        public KeeperConfig(string path)
        {
            _path = path;
            Load();
        }

        public string Path { get { return _path; } }

        public string Get(string key, string defaultValue)
        {
            lock (_sync)
            {
                string value;
                if (_values.TryGetValue(key, out value)) return value;
                _values[key] = defaultValue;
                SaveUnsafe();
                return defaultValue;
            }
        }

        public bool GetBool(string key, bool defaultValue)
        {
            bool parsed;
            return bool.TryParse(Get(key, defaultValue.ToString()), out parsed) ? parsed : defaultValue;
        }

        public int GetInt(string key, int defaultValue, int minimum, int maximum)
        {
            int parsed;
            if (!int.TryParse(Get(key, defaultValue.ToString(CultureInfo.InvariantCulture)),
                NumberStyles.Integer, CultureInfo.InvariantCulture, out parsed)) parsed = defaultValue;
            return Math.Max(minimum, Math.Min(maximum, parsed));
        }

        public float GetFloat(string key, float defaultValue, float minimum, float maximum)
        {
            float parsed;
            if (!float.TryParse(Get(key, defaultValue.ToString(CultureInfo.InvariantCulture)),
                NumberStyles.Float, CultureInfo.InvariantCulture, out parsed)) parsed = defaultValue;
            return Math.Max(minimum, Math.Min(maximum, parsed));
        }

        public void Set(string key, object value)
        {
            lock (_sync)
            {
                _values[key] = Convert.ToString(value, CultureInfo.InvariantCulture);
                SaveUnsafe();
            }
        }

        private void Load()
        {
            lock (_sync)
            {
                _values.Clear();
                if (!File.Exists(_path)) return;
                string[] lines = File.ReadAllLines(_path);
                for (int i = 0; i < lines.Length; i++)
                {
                    string line = lines[i].Trim();
                    if (line.Length == 0 || line.StartsWith("#") || line.StartsWith(";")) continue;
                    int equals = line.IndexOf('=');
                    if (equals <= 0) continue;
                    _values[line.Substring(0, equals).Trim()] = line.Substring(equals + 1).Trim();
                }
            }
        }

        private void SaveUnsafe()
        {
            string directory = System.IO.Path.GetDirectoryName(_path);
            if (!Directory.Exists(directory)) Directory.CreateDirectory(directory);
            string temporary = _path + ".tmp";
            List<string> keys = new List<string>(_values.Keys);
            keys.Sort(StringComparer.OrdinalIgnoreCase);
            using (StreamWriter writer = new StreamWriter(temporary, false))
            {
                writer.WriteLine("# KeeperLoader mod configuration");
                for (int i = 0; i < keys.Count; i++)
                    writer.WriteLine(keys[i] + "=" + _values[keys[i]]);
            }
            if (File.Exists(_path)) File.Delete(_path);
            File.Move(temporary, _path);
        }
    }
}
