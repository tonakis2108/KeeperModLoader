using System;
using System.IO;
using System.Reflection;
using UnityEngine;

namespace KeeperLoader.Runtime
{
    public static class RuntimeHost
    {
        private static bool _created;

        public static void Attach()
        {
            if (_created) return;
            CreateHost();
        }

        private static void CreateHost()
        {
            if (_created) return;
            GameObject existing = GameObject.Find("KeeperLoader Runtime");
            if (existing != null)
            {
                _created = true;
                return;
            }
            GameObject host = new GameObject("KeeperLoader Runtime");
            host.hideFlags = HideFlags.HideAndDontSave;
            UnityEngine.Object.DontDestroyOnLoad(host);
            host.AddComponent<LoaderHost>();
            _created = true;
        }
    }

    internal sealed class LoaderHost : MonoBehaviour
    {
        private const string BadgeTooltip = "KeeperLoader activated";
        private const string SafeModeBadgeTooltip = "KeeperLoader activated in safe mode; mods are paused";
        private ModCatalog _catalog;
        private FileLogger _log;
        private string _loaderDirectory;
        private bool _safeMode;
        private float _badgeVisibleUntil;
        private GUIStyle _badgeStyle;
        private GUIStyle _badgeTooltipStyle;

        private void Awake()
        {
            _loaderDirectory = Environment.GetEnvironmentVariable("KEEPERLOADER_DIR");
            if (string.IsNullOrEmpty(_loaderDirectory))
                _loaderDirectory = Path.Combine(Environment.CurrentDirectory, "KeeperLoader");
            _log = new FileLogger(Path.Combine(_loaderDirectory, "logs", "latest.log"), "Core");
            _badgeVisibleUntil = Time.realtimeSinceStartup + 30f;
            _safeMode = string.Equals(Environment.GetEnvironmentVariable("KEEPERLOADER_SAFE_MODE"), "1");
        }

        private void Start()
        {
            // Unity calls Start on the next normal lifecycle pass. This keeps
            // mod startup deferred until the engine is ready without relying
            // on SceneManager events that older Graveyard Keeper builds lack.
            _log.Info("KeeperLoader runtime 0.7.4 initialized for " +
                Environment.GetEnvironmentVariable("KEEPERLOADER_GAME_ID") +
                (_safeMode ? " in SAFE MODE." : "."));
            _catalog = new ModCatalog(_loaderDirectory, _log);
            if (!_safeMode) _catalog.DiscoverAndLoad();
            else _log.Warning("Mods were skipped for this user-selected safe-mode launch only.");
        }

        private void Update()
        {
            if (_catalog != null) _catalog.Update();
        }

        private void OnGUI()
        {
            if (_catalog != null) _catalog.DrawGUI();
            DrawActivationBadge();
        }

        private void DrawActivationBadge()
        {
            if (!ShouldShowActivationBadge()) return;
            if (_badgeStyle == null)
            {
                _badgeStyle = new GUIStyle(GUI.skin.box);
                _badgeStyle.alignment = TextAnchor.MiddleCenter;
                _badgeStyle.fontSize = 11;
                _badgeStyle.fontStyle = FontStyle.Bold;
                _badgeStyle.normal.textColor = Color.white;
                _badgeTooltipStyle = new GUIStyle(GUI.skin.box);
                _badgeTooltipStyle.alignment = TextAnchor.MiddleCenter;
                _badgeTooltipStyle.fontSize = 11;
                _badgeTooltipStyle.normal.textColor = Color.white;
            }

            Color previousBackground = GUI.backgroundColor;
            GUI.backgroundColor = _safeMode
                ? new Color(0.82f, 0.55f, 0.12f, 0.96f)
                : new Color(0.18f, 0.62f, 0.34f, 0.96f);
            Rect badge = new Rect(Screen.width - 76f, 12f, 64f, 26f);
            string tooltipText = _safeMode ? SafeModeBadgeTooltip : BadgeTooltip;
            GUI.Box(badge, new GUIContent(_safeMode ? "KL !" : "KL \u2713", tooltipText), _badgeStyle);
            GUI.backgroundColor = previousBackground;

            if (GUI.tooltip == tooltipText)
            {
                float width = _safeMode ? 310f : 178f;
                Rect tooltip = new Rect(Screen.width - width - 12f, 42f, width, 26f);
                GUI.Box(tooltip, tooltipText, _badgeTooltipStyle);
            }
        }

        private bool ShouldShowActivationBadge()
        {
            if (Time.realtimeSinceStartup <= _badgeVisibleUntil) return true;
            string sceneName = ActiveSceneName();
            if (string.IsNullOrEmpty(sceneName)) return false;
            return SceneNameContains(sceneName, "menu") ||
                   SceneNameContains(sceneName, "title") ||
                   SceneNameContains(sceneName, "frontend") ||
                   SceneNameContains(sceneName, "lobby");
        }

        private static string ActiveSceneName()
        {
            // Some Graveyard Keeper releases expose SceneManager without the
            // sceneLoaded event used by newer Unity versions. Reflection keeps
            // that optional API out of the runtime's hard assembly references.
            try
            {
                Type sceneManager = Type.GetType(
                    "UnityEngine.SceneManagement.SceneManager, UnityEngine.CoreModule", false) ??
                    Type.GetType("UnityEngine.SceneManagement.SceneManager, UnityEngine", false);
                if (sceneManager != null)
                {
                    MethodInfo getActiveScene = sceneManager.GetMethod("GetActiveScene",
                        BindingFlags.Public | BindingFlags.Static, null, Type.EmptyTypes, null);
                    if (getActiveScene != null)
                    {
                        object scene = getActiveScene.Invoke(null, null);
                        if (scene != null)
                        {
                            PropertyInfo name = scene.GetType().GetProperty("name",
                                BindingFlags.Public | BindingFlags.Instance);
                            if (name != null) return name.GetValue(scene, null) as string ?? string.Empty;
                        }
                    }
                }

                Type application = Type.GetType("UnityEngine.Application, UnityEngine.CoreModule", false) ??
                                   Type.GetType("UnityEngine.Application, UnityEngine", false);
                if (application != null)
                {
                    PropertyInfo legacyName = application.GetProperty("loadedLevelName",
                        BindingFlags.Public | BindingFlags.Static);
                    if (legacyName != null)
                        return legacyName.GetValue(null, null) as string ?? string.Empty;
                }
            }
            catch
            {
                // The scene name controls only the optional menu badge. Mod
                // loading must never depend on a particular Unity scene API.
            }
            return string.Empty;
        }

        private static bool SceneNameContains(string sceneName, string value)
        {
            return sceneName.IndexOf(value, StringComparison.OrdinalIgnoreCase) >= 0;
        }

        private void OnApplicationQuit()
        {
            if (_catalog != null) _catalog.UnloadAll();
        }
    }
}
