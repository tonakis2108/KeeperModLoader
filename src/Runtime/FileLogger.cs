using System;
using System.IO;
using KeeperLoader.API;

namespace KeeperLoader.Runtime
{
    internal sealed class FileLogger : IKeeperLogger
    {
        private static readonly object Sync = new object();
        private readonly string _path;
        private readonly string _source;

        public FileLogger(string path, string source)
        {
            _path = path;
            _source = source;
        }

        public void Info(string message) { Write("Info", message); }
        public void Warning(string message) { Write("Warning", message); }
        public void Error(string message) { Write("Error", message); }
        public void Error(string message, Exception exception) { Write("Error", message + Environment.NewLine + exception); }

        private void Write(string level, string message)
        {
            string line = DateTime.Now.ToString("HH:mm:ss.fff") + " [" + level + ":" + _source + "] " + message;
            lock (Sync)
            {
                try { File.AppendAllText(_path, line + Environment.NewLine); } catch { }
                try { Console.WriteLine(line); } catch { }
            }
        }
    }
}
