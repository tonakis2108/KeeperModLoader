// Compile-only Unity API facade for CI release builds.
// This assembly is never distributed. Graveyard Keeper supplies the real assembly.
namespace UnityEngine
{
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
