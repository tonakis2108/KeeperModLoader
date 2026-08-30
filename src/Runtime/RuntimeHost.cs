using System;
using System.IO;
using UnityEngine;
using UnityEngine.SceneManagement;

namespace KeeperLoader.Runtime
{
    public static class RuntimeHost
    {
        private static bool _scheduled;
        private static bool _created;

        public static void Attach()
        {
            if (_scheduled || _created) return;
            _scheduled = true;
            SceneManager.sceneLoaded += OnFirstSceneLoaded;
        }

        private static void OnFirstSceneLoaded(Scene scene, LoadSceneMode mode)
        {
            SceneManager.sceneLoaded -= OnFirstSceneLoaded;
            _scheduled = false;
            CreateHost();
        }

        private static void CreateHost()
        {
            if (_created) return;
            _created = true;
            GameObject existing = GameObject.Find("KeeperLoader Runtime");
            if (existing != null) return;
            GameObject host = new GameObject("KeeperLoader Runtime");
            host.hideFlags = HideFlags.HideAndDontSave;
            UnityEngine.Object.DontDestroyOnLoad(host);
            host.AddComponent<LoaderHost>();
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
            _log.Info("KeeperLoader runtime 0.7.0 initialized for " +
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
            string sceneName = SceneManager.GetActiveScene().name;
            if (string.IsNullOrEmpty(sceneName)) return false;
            return SceneNameContains(sceneName, "menu") ||
                   SceneNameContains(sceneName, "title") ||
                   SceneNameContains(sceneName, "frontend") ||
                   SceneNameContains(sceneName, "lobby");
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
