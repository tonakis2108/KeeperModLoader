// Compile-only Unity API facade for CI release builds.
// This assembly is never distributed. Graveyard Keeper supplies the real assembly.
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
        public T AddComponent<T>() where T : Component { return default(T); }
        public Component AddComponent(Type type) { return null; }
    }

    public enum HideFlags { HideAndDontSave }
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
