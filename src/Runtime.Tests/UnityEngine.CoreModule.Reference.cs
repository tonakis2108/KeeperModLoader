// Compile-only Unity API facade for CI release builds.
// This assembly is never distributed. Graveyard Keeper supplies the real assembly.
using System;

namespace UnityEngine
{
    public static class Application
    {
        public static event Action onBeforeRender;
        public static string loadedLevelName { get { return string.Empty; } }

        public static void RaiseBeforeRender()
        {
            Action handler = onBeforeRender;
            if (handler != null) handler();
        }
    }

    public class Object
    {
        public HideFlags hideFlags { get; set; }
        public static void DontDestroyOnLoad(Object target) { }
        public static void Destroy(Object target) { }
    }

    public class Component : Object { }
    public class MonoBehaviour : Component { }

    public sealed class GameObject : Object
    {
        private static GameObject _lastCreated;
        private readonly string _name;

        public GameObject(string name) { _name = name; _lastCreated = this; }
        public static GameObject Find(string name)
        {
            return _lastCreated != null && _lastCreated._name == name ? _lastCreated : null;
        }
        public T AddComponent<T>() where T : Component { return default(T); }
        public Component AddComponent(Type type) { return null; }
    }

    public sealed class Camera : Component
    {
        public delegate void CameraCallback(Camera camera);
        public static event CameraCallback onPreCull;

        public static void RaisePreCull()
        {
            CameraCallback handler = onPreCull;
            if (handler != null) handler(null);
        }
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
