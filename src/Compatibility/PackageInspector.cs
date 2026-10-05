using System;
using System.Collections.Generic;
using System.IO;
using System.Web.Script.Serialization;
using Mono.Cecil;

// Trusted, precompiled metadata reader. Never Assembly.Load, instantiate attributes,
// invoke mod code, or resolve dependencies outside the staged package.
internal static class PackageInspector
{
    private static int Main(string[] args)
    {
        try
        {
            if (args.Length != 1) throw new InvalidDataException("Expected one staged package directory.");
            string root = Path.GetFullPath(args[0]);
            var plugins = new List<object>();
            var assemblies = new List<object>();
            bool native = false;
            string[] files = Directory.GetFiles(root, "*.dll", SearchOption.AllDirectories);
            if (files.Length > 128) throw new InvalidDataException("Too many assemblies.");
            foreach (string path in files)
            {
                if (new FileInfo(path).Length > 32 * 1024 * 1024) throw new InvalidDataException("Assembly exceeds 32 MB.");
                using (AssemblyDefinition assembly = AssemblyDefinition.ReadAssembly(path,
                    new ReaderParameters { ReadingMode = ReadingMode.Deferred, ReadSymbols = false,
                        AssemblyResolver = new RuntimeMetadataResolver() }))
                {
                    if (assembly.Modules.Count != 1) throw new InvalidDataException("Multi-module assemblies are not supported.");
                    if ((assembly.MainModule.Attributes & ModuleAttributes.ILOnly) == 0)
                        throw new InvalidDataException("Native or mixed-mode DLLs are not supported.");
                    assemblies.Add(new { path = path.Substring(root.Length + 1).Replace('\\', '/'),
                        name = assembly.Name.Name, version = assembly.Name.Version.ToString() });
                    foreach (AssemblyNameReference reference in assembly.MainModule.AssemblyReferences)
                    {
                        if (reference.Name == "KeeperLoader.API") native = true;
                        if (reference.Name.StartsWith("BepInEx") && (reference.Name != "BepInEx" || reference.Version.Major != 5))
                            throw new InvalidDataException("Only BepInEx 5 Unity Mono is supported.");
                    }
                    foreach (TypeDefinition type in AllTypes(assembly.MainModule.Types))
                    {
                        if (type.FullName.StartsWith("KeeperLoader.API.") || type.FullName.StartsWith("BepInEx."))
                            throw new InvalidDataException("Packages must not redefine loader APIs.");
                        foreach (MethodDefinition method in type.Methods)
                            if (method.IsStatic && method.Name == "Patch" && method.Parameters.Count == 1 &&
                                method.Parameters[0].ParameterType.FullName == "Mono.Cecil.AssemblyDefinition")
                                throw new InvalidDataException("Preloader patchers are not supported.");
                        CustomAttribute metadata = null;
                        var dependencies = new List<object>();
                        var processes = new List<string>();
                        var incompatible = new List<string>();
                        foreach (CustomAttribute attribute in type.CustomAttributes)
                        {
                            string name = attribute.AttributeType.FullName;
                            if (name == "KeeperLoader.API.KeeperModAttribute") native = true;
                            if (!name.StartsWith("BepInEx.")) continue;
                            var scope = attribute.AttributeType.Scope as AssemblyNameReference;
                            if (scope == null || scope.Name != "BepInEx" || scope.Version.Major != 5)
                                throw new InvalidDataException("Invalid BepInEx metadata scope.");
                            if (name == "BepInEx.BepInPlugin") metadata = attribute;
                            else if (name == "BepInEx.BepInProcess") processes.Add((string)attribute.ConstructorArguments[0].Value);
                            else if (name == "BepInEx.BepInIncompatibility") incompatible.Add((string)attribute.ConstructorArguments[0].Value);
                            else if (name == "BepInEx.BepInDependency")
                            {
                                object value = attribute.ConstructorArguments[1].Value;
                                dependencies.Add(new { id = (string)attribute.ConstructorArguments[0].Value,
                                    minimumVersion = value is string ? (string)value : "0.0.0.0",
                                    hard = value is string || (Convert.ToInt32(value) & 1) != 0 });
                            }
                        }
                        if (metadata == null) continue;
                        if (type.IsAbstract || !type.IsPublic || type.HasGenericParameters || !IsPlugin(type, assembly.MainModule))
                            throw new InvalidDataException("Plugin must be a public, concrete BepInEx 5 BaseUnityPlugin.");
                        plugins.Add(new { id = (string)metadata.ConstructorArguments[0].Value,
                            name = (string)metadata.ConstructorArguments[1].Value,
                            version = (string)metadata.ConstructorArguments[2].Value,
                            dependencies = dependencies, processes = processes, incompatible = incompatible });
                    }
                }
            }
            Console.Write(new JavaScriptSerializer().Serialize(new { native = native, plugins = plugins, assemblies = assemblies }));
            return 0;
        }
        catch (Exception error) { Console.Error.Write(error.Message); return 1; }
    }

    private static bool IsPlugin(TypeDefinition type, ModuleDefinition module)
    {
        var visited = new HashSet<string>();
        TypeReference parent = type.BaseType;
        while (parent != null && visited.Add(parent.FullName))
        {
            var scope = parent.Scope as AssemblyNameReference;
            if (parent.FullName == "BepInEx.BaseUnityPlugin")
                return scope != null && scope.Name == "BepInEx" && scope.Version.Major == 5;
            // Deliberately do not resolve external assemblies, even via Cecil.
            TypeDefinition local = module.GetType(parent.FullName);
            if (local == null) return false;
            parent = local.BaseType;
        }
        return false;
    }

    private static IEnumerable<TypeDefinition> AllTypes(IEnumerable<TypeDefinition> types)
    {
        foreach (TypeDefinition type in types)
        {
            yield return type;
            foreach (TypeDefinition nested in AllTypes(type.NestedTypes)) yield return nested;
        }
    }

    private sealed class RuntimeMetadataResolver : IAssemblyResolver
    {
        private AssemblyDefinition _bep;
        public AssemblyDefinition Resolve(AssemblyNameReference name) { return Resolve(name, new ReaderParameters()); }
        public AssemblyDefinition Resolve(AssemblyNameReference name, ReaderParameters parameters)
        {
            // Cecil needs the enum definition to decode BepInDependency flags.
            // Resolve only the pinned official API, never an arbitrary plugin.
            if (name.Name != "BepInEx" || name.Version.Major != 5)
                throw new AssemblyResolutionException(name);
            if (_bep == null)
            {
                string tools = AppDomain.CurrentDomain.BaseDirectory;
                string api = Path.GetFullPath(Path.Combine(tools, "..", "runtime", "BepInEx", "core", "BepInEx.dll"));
                _bep = AssemblyDefinition.ReadAssembly(api, new ReaderParameters { ReadSymbols = false,
                    AssemblyResolver = this });
            }
            return _bep;
        }
        public void Dispose() { if (_bep != null) _bep.Dispose(); }
    }
}
