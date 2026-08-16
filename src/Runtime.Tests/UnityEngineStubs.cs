// Minimal compile-time surface used by GitHub Actions. Real builds reference the selected game's Unity assemblies.
using System;

namespace UnityEngine
{
    public class Object
    {
        public static void DontDestroyOnLoad(Object target) { }
        public static void Destroy(Object target) { }
    }

    public class Component : Object { }
    public class MonoBehaviour : Component { }

    public sealed class GameObject : Object
    {
        public GameObject(string name) { }
        public HideFlags hideFlags;
        public static GameObject Find(string name) { return null; }
        public T AddComponent<T>() where T : Component, new() { return new T(); }
        public Component AddComponent(Type type) { return null; }
    }

    public enum HideFlags { HideAndDontSave }
    public enum TextAnchor { MiddleCenter }
    public enum FontStyle { Bold }

    public struct Color
    {
        public Color(float red, float green, float blue, float alpha) { }
        public static Color white { get { return new Color(); } }
    }

    public struct Rect
    {
        public Rect(float x, float y, float width, float height) { }
    }

    public static class Time
    {
        public static float realtimeSinceStartup { get { return 0f; } }
    }

    public static class Screen
    {
        public static int width { get { return 1920; } }
    }

    public sealed class GUIContent
    {
        public GUIContent(string text, string tooltip) { }
    }

    public sealed class GUIStyleState
    {
        public Color textColor;
    }

    public sealed class GUIStyle
    {
        public GUIStyle(GUIStyle source) { normal = new GUIStyleState(); }
        public TextAnchor alignment;
        public int fontSize;
        public FontStyle fontStyle;
        public GUIStyleState normal;
    }

    public sealed class GUISkin
    {
        public GUIStyle box { get { return new GUIStyle(null); } }
    }

    public static class GUI
    {
        public static Color backgroundColor;
        public static string tooltip { get { return ""; } }
        public static GUISkin skin { get { return new GUISkin(); } }
        public static void Box(Rect position, GUIContent content, GUIStyle style) { }
        public static void Box(Rect position, string text, GUIStyle style) { }
    }
}

namespace UnityEngine.SceneManagement
{
    public struct Scene
    {
        public string name { get { return ""; } }
    }

    public enum LoadSceneMode { Single, Additive }

    public static class SceneManager
    {
        public static event Action<Scene, LoadSceneMode> sceneLoaded;
        public static Scene GetActiveScene() { return new Scene(); }
    }
}
