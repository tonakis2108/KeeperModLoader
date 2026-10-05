package main

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "sort"
 "strings"
 "syscall"
 "time"
)

const bepMode = "bepinex-v5"
const bepHostID = "keeperloader.nativehost"
const bepMarker = "bepinex5.enabled"
const bepRecord = "keeperloader.bepinex.json"

func validPluginID(id string) bool {
 if !modIDPattern.MatchString(id) || strings.HasPrefix(id, ".") { return false }
 _, err := normalizedArchivePath(id)
 return err == nil && !strings.EqualFold(id,bepHostID)
}

type pluginDependency struct {
 ID string `json:"id"`
 MinimumVersion string `json:"minimumVersion"`
 Hard bool `json:"hard"`
}
type pluginMetadata struct {
 ID string `json:"id"`
 Name string `json:"name"`
 Version string `json:"version"`
 Dependencies []pluginDependency `json:"dependencies"`
 Processes []string `json:"processes"`
 Incompatible []string `json:"incompatible"`
}
type pluginAssembly struct {
 Path string `json:"path"`
 Name string `json:"name"`
 Version string `json:"version"`
}
type packageInspection struct {
 Native bool `json:"native"`
 Plugins []pluginMetadata `json:"plugins"`
 Assemblies []pluginAssembly `json:"assemblies"`
}
type pluginRecord struct {
 Type string `json:"type"`
 GameID string `json:"gameId"`
 Plugin pluginMetadata `json:"plugin"`
 Assemblies []pluginAssembly `json:"assemblies"`
 Files []ManifestFile `json:"files"`
}

func bepRoot(game *GameInfo) string { return filepath.Join(game.GameDirectory, "BepInEx") }
func bepManagedRoot(game *GameInfo) string { return filepath.Join(bepRoot(game), "plugins", "KeeperLoaderManaged") }
func bepDisabledRoot(game *GameInfo) string { return filepath.Join(game.GameDirectory, "KeeperLoader", "external-disabled") }
func compatibilityEnabled(game *GameInfo) bool {
 return fileExists(filepath.Join(game.GameDirectory, "KeeperLoader", "state", bepMarker))
}
func compatibilityOwned(game *GameInfo) bool {
 return fileExists(filepath.Join(bepRoot(game), ".keeperloader-owned"))
}

func inspectPackage(root string) (*packageInspection, error) {
 payload, err := runtimePayloadRoot()
 if err != nil { return nil, err }
 if err = validateRuntimePayload(payload); err != nil { return nil, err }
 inspector := filepath.Join(payload, "compatibility", "tools", "KeeperLoader.PackageInspector.exe")
 if !fileExists(inspector) { return nil, errors.New("extract the complete compatibility-enabled manager ZIP; metadata inspector is missing") }
 ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
 defer cancel()
 command := exec.CommandContext(ctx, inspector, root)
 command.Dir = filepath.Dir(inspector)
 command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
 var output, stderr limitedInspectionOutput
 command.Stdout, command.Stderr = &output, &stderr
 if err = command.Run(); err != nil {
  return nil, fmt.Errorf("metadata inspection rejected package: %s (%v)", stderr.String(), err)
 }
 var result packageInspection
 if err = json.Unmarshal(output.Bytes(), &result); err != nil { return nil, errors.New("metadata inspector returned an invalid result") }
 return &result, nil
}
type limitedInspectionOutput struct { bytes.Buffer }
func (b *limitedInspectionOutput) Write(p []byte) (int, error) {
 if b.Len()+len(p)>1024*1024 { return 0, errors.New("metadata output exceeded 1 MB") }
 return b.Buffer.Write(p)
}

func enableCompatibility(game *GameInfo) (string, error) {
 if err := assertGameStopped(game); err != nil { return "", err }
 if !game.Supported || game.GameID != graveyardKeeperGameID || game.Architecture != "x64" || game.Backend != "Mono" {
  return "", errors.New("compatibility currently supports only Windows x64 Unity Mono Graveyard Keeper installations")
 }
 if !loaderEnabled(game) || installedLoaderVersion(game) != loaderVersion {
  return "", errors.New("install / update KeeperLoader for this game first")
 }
 payload, err := runtimePayloadRoot()
 if err != nil { return "", err }
 if err = validateRuntimePayload(payload); err != nil { return "", err }
 source := filepath.Join(payload, "compatibility", "runtime", "BepInEx")
 if !fileExists(filepath.Join(source, "core", "BepInEx.Preloader.dll")) {
  return "", errors.New("optional compatibility payload is missing; extract the full manager ZIP")
 }
 if err = checkNativeLibraries(game, nil, true); err != nil { return "", err }
 if dirExists(bepRoot(game)) {
  if !compatibilityOwned(game) { return "", errors.New("an existing BepInEx installation was found; it will not be overwritten or adopted automatically") }
  // Do not run arbitrary plugins or patchers installed outside our ownership.
  if err = verifyCompatibilityTree(game, source); err != nil { return "", err }
 } else {
  staging, stageErr := os.MkdirTemp(game.GameDirectory, ".bepinex-install-")
  if stageErr != nil { return "", stageErr }
  defer os.RemoveAll(staging)
  if err = copyDirectory(source, staging); err != nil { return "", err }
  if err = writeAtomic(filepath.Join(staging, ".keeperloader-owned"), []byte("BepInEx 5.4.23.5 managed by KeeperLoader\n"), 0644); err != nil { return "", err }
  if err = os.Rename(staging, bepRoot(game)); err != nil { return "", err }
 }
 marker := filepath.Join(game.GameDirectory, "KeeperLoader", "state", bepMarker)
 if err = writeAtomic(marker, []byte("explicit_user_opt_in=true\n"), 0644); err != nil { return "", err }
 return "BepInEx 5 compatibility enabled. One existing Doorstop bootstrap dispatches both runtimes. Restart the game.", nil
}

func verifyCompatibilityTree(game *GameInfo, source string) error {
 if entries, err := os.ReadDir(filepath.Join(bepRoot(game), "patchers")); err == nil && len(entries)>0 {
  return errors.New("preloader patchers are not supported; compatibility was not enabled")
 }
 if err := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
  if err != nil { return err }; if info.IsDir() { return nil }
  rel, err := filepath.Rel(source,path); if err != nil { return err }
  wanted,e1 := fileSHA256(path); actual,e2 := fileSHA256(filepath.Join(bepRoot(game),rel))
  if e1 != nil || e2 != nil || wanted != actual { return fmt.Errorf("missing or modified official compatibility file: %s",rel) }
  return nil
 }); err != nil { return err }
 mods,err := installedBepPlugins(game);if err != nil { return err }
 for _,mod := range mods { if !mod.Enabled { continue };record,err:=readPluginRecord(mod.Path);if err != nil { return err };if err=verifyPluginFiles(mod.Path,record);err!=nil{return err};if err=checkPluginConflicts(game,record,mod.ID);err!=nil{return err} }
 return filepath.Walk(bepRoot(game), func(path string, info os.FileInfo, err error) error {
  if err != nil { return err }
  if info.Mode()&os.ModeSymlink != 0 { return errors.New("symbolic links/reparse points are not supported") }
  rel, err := filepath.Rel(bepRoot(game), path)
  if err != nil || info.IsDir() { return err }
  normalized := filepath.ToSlash(rel)
  if strings.EqualFold(filepath.Ext(rel), ".dll") {
   if strings.HasPrefix(normalized, "plugins/KeeperLoaderManaged/") {
    parts := strings.Split(strings.TrimPrefix(normalized, "plugins/KeeperLoaderManaged/"), "/")
    if len(parts) < 2 { return errors.New("loose DLLs in the managed plugin root are not supported") }
    if !strings.EqualFold(parts[0], bepHostID) {
     if !fileExists(filepath.Join(bepManagedRoot(game), parts[0], bepRecord)) { return errors.New("unregistered managed plugin DLL rejected") }
     return nil
    }
   }
   expected := filepath.Join(source, rel)
   wanted, e1 := fileSHA256(expected); actual, e2 := fileSHA256(path)
   if e1 != nil || e2 != nil || wanted != actual { return fmt.Errorf("unexpected or modified compatibility DLL: %s", rel) }
  }
  return nil
 })
}

func restoreNativeMode(game *GameInfo) (string, error) {
 if err := assertGameStopped(game); err != nil { return "", err }
 path := filepath.Join(game.GameDirectory, "KeeperLoader", "state", bepMarker)
 if err := os.Remove(path); err != nil && !os.IsNotExist(err) { return "", err }
 return "Native mode restored. External plugins will not load; all native mods, external packages and configurations were preserved.", nil
}

func removeCompatibility(game *GameInfo) (string, error) {
 if err := assertGameStopped(game); err != nil { return "", err }
 if !compatibilityOwned(game) { return "", errors.New("no KeeperLoader-owned compatibility runtime is installed") }
 mods, err := installedBepPlugins(game); if err != nil { return "", err }
 if len(mods)>0 { return "", errors.New("uninstall all managed external plugins before removing compatibility; their configurations will be preserved") }
 if _, err = restoreNativeMode(game); err != nil { return "", err }
 backup := uniqueTimestampPath(game.GameDirectory, "BepInEx-KeeperLoader-preserved")
 if err = os.Rename(bepRoot(game), backup); err != nil { return "", err }
 return "Native mode restored. Compatibility runtime and its configuration were moved, not deleted: " + backup, nil
}

func readPluginRecord(root string) (*pluginRecord, error) {
 data, err := os.ReadFile(filepath.Join(root, bepRecord))
 if err != nil { return nil, err }
 var record pluginRecord
 if json.Unmarshal(data, &record)!=nil || record.Type!=bepMode || !validPluginID(record.Plugin.ID) {
  return nil, errors.New("invalid external package record")
 }
 return &record, nil
}
func installedBepPlugins(game *GameInfo) ([]*InstalledMod, error) {
 var result []*InstalledMod
 for _, root := range []string{bepManagedRoot(game), bepDisabledRoot(game)} {
  entries, err := os.ReadDir(root)
  if os.IsNotExist(err) { continue }; if err != nil { return nil, err }
  for _, entry := range entries {
   if !entry.IsDir() || entry.Name()==bepHostID || strings.HasPrefix(entry.Name(), ".") { continue }
   path := filepath.Join(root, entry.Name())
   record, err := readPluginRecord(path)
   if err != nil { return nil, fmt.Errorf("external registry requires attention at %s: %w", path, err) }
   if record.GameID != game.GameID || record.Plugin.ID != entry.Name() { return nil, errors.New("external registry identity mismatch") }
   status := "Installed; restart required. See BepInEx/LogOutput.log for load errors."
   if !compatibilityEnabled(game) { status = "Compatibility inactive" }
   result = append(result, &InstalledMod{ID:record.Plugin.ID, Name:record.Plugin.Name, Version:record.Plugin.Version, Path:path,
    Enabled:root==bepManagedRoot(game), Mode:bepMode, Status:status})
  }
 }
 return result, nil
}

func validateBepSelection(game *GameInfo, mod *InstalledMod) error {
 if mod==nil || mod.Mode!=bepMode || !validPluginID(mod.ID) { return errors.New("invalid external plugin selection") }
 expected := filepath.Join(bepDisabledRoot(game), mod.ID)
 if mod.Enabled { expected = filepath.Join(bepManagedRoot(game), mod.ID) }
 if !strings.EqualFold(filepath.Clean(mod.Path), filepath.Clean(expected)) { return errors.New("external plugin path is outside its managed directory") }
 record, err := readPluginRecord(mod.Path)
 if err != nil { return err }; if record.Plugin.ID!=mod.ID || record.GameID!=game.GameID { return errors.New("plugin record mismatch") }
 return nil
}

func validatePluginMetadata(game *GameInfo, inspection *packageInspection) (*pluginMetadata, error) {
 if inspection.Native || len(inspection.Plugins)!=1 { return nil, errors.New("package must contain exactly one BepInEx 5 plugin and no native KeeperLoader mods") }
 plugin := &inspection.Plugins[0]
 if !validPluginID(plugin.ID) || strings.TrimSpace(plugin.Name)=="" { return nil, errors.New("invalid or reserved plugin identity") }
 if _,err:=parseVersion(plugin.Version); err!=nil { return nil, errors.New("invalid numeric plugin version") }
 if len(plugin.Processes)>0 {
  match:=false
  for _, process:=range plugin.Processes { if strings.EqualFold(strings.TrimSuffix(strings.ToLower(process),".exe"),strings.TrimSuffix(strings.ToLower(game.ExecutableName),".exe")) { match=true } }
  if !match { return nil, errors.New("plugin process filter does not support the selected game") }
 }
 seen:=map[string]bool{}
 for _,assembly:=range inspection.Assemblies {
  name:=strings.ToLower(assembly.Name)
  if seen[name] { return nil,fmt.Errorf("duplicate assembly identity %s",assembly.Name) };seen[name]=true
  if reservedPluginAssembly(name) { return nil,fmt.Errorf("package may not supply shared runtime or game assembly %s",assembly.Name) }
 }
 return plugin,nil
}
func reservedPluginAssembly(name string) bool {
 return name=="0harmony" || name=="mscorlib" || name=="netstandard" || name=="assembly-csharp" || name=="assembly-csharp-firstpass" ||
 strings.HasPrefix(name,"bepinex") || strings.HasPrefix(name,"keeperloader") || strings.HasPrefix(name,"unity") || strings.HasPrefix(name,"mono.") || strings.HasPrefix(name,"monomod") || strings.HasPrefix(name,"system")
}

func checkPluginConflicts(game *GameInfo, incoming *pluginRecord, exclude string) error {
 if err := checkNativeLibraries(game, incoming.Assemblies, false); err != nil { return err }
 installed,err:=installedBepPlugins(game);if err!=nil{return err}
 available:=map[string]string{bepHostID:loaderVersion}
 graph:=map[string][]string{}
 for _,mod:=range installed {
  if !mod.Enabled || mod.ID==exclude {continue}
  available[mod.ID]=mod.Version
  record,err:=readPluginRecord(mod.Path);if err!=nil{return err}
  for _,dep:=range record.Plugin.Dependencies {
   if dep.Hard && dep.ID==incoming.Plugin.ID {
    target,e1:=parseVersion(incoming.Plugin.Version);minimum,e2:=parseVersion(dep.MinimumVersion)
    if e1!=nil || e2!=nil || compareVersion(target,minimum)<0 {return fmt.Errorf("%s requires this plugin at version %s or newer",mod.Name,dep.MinimumVersion)}
   }
  }
  for _,d:=range record.Plugin.Dependencies { if d.Hard {graph[mod.ID]=append(graph[mod.ID],d.ID)} }
  for _,id:=range record.Plugin.Incompatible {if id==incoming.Plugin.ID{return fmt.Errorf("%s declares this plugin incompatible",mod.Name)}}
  for _,a:=range record.Assemblies {for _,b:=range incoming.Assemblies {if strings.EqualFold(a.Name,b.Name){return fmt.Errorf("assembly %s is already supplied by %s; shared libraries are not automatically replaced",b.Name,mod.Name)}}}
 }
 for _,id:=range incoming.Plugin.Incompatible {if _,ok:=available[id];ok{return fmt.Errorf("incompatible enabled plugin: %s",id)}}
 for _,dep:=range incoming.Plugin.Dependencies {
  if !dep.Hard {continue}; version,ok:=available[dep.ID];if !ok{return fmt.Errorf("missing enabled hard dependency: %s",dep.ID)}
  actual,e1:=parseVersion(version);minimum,e2:=parseVersion(dep.MinimumVersion)
  if e1!=nil || e2!=nil || compareVersion(actual,minimum)<0{return fmt.Errorf("dependency %s needs version %s or newer",dep.ID,dep.MinimumVersion)}
  graph[incoming.Plugin.ID]=append(graph[incoming.Plugin.ID],dep.ID)
 }
 visiting,done:=map[string]bool{},map[string]bool{}
 var visit func(string)bool
 visit=func(id string)bool {if visiting[id]{return false};if done[id]{return true};visiting[id]=true;for _,dep:=range graph[id]{if !visit(dep){return false}};visiting[id]=false;done[id]=true;return true}
 for id:=range graph {if !visit(id){return errors.New("hard dependency cycle rejected")}}
 return nil
}

func checkNativeLibraries(game *GameInfo, incoming []pluginAssembly, compatibilityStart bool) error {
 mods,err:=installedMods(game);if err!=nil{return err}
 for _,mod:=range mods {
  if !mod.Enabled || mod.Mode!= "native" {continue}
  inspection,err:=inspectPackage(mod.Path);if err!=nil{return fmt.Errorf("cannot verify native mod %s before compatibility: %w",mod.Name,err)}
  if len(inspection.Plugins)>0{return fmt.Errorf("native mod %s contains external plugin metadata",mod.Name)}
  for _,a:=range inspection.Assemblies {
   name:=strings.ToLower(a.Name)
   if compatibilityStart && (name=="0harmony" || strings.HasPrefix(name,"monomod") || strings.HasPrefix(name,"mono.cecil") || strings.HasPrefix(name,"bepinex")) {
    return fmt.Errorf("native mod %s bundles a shared runtime library %s; compatibility will not replace or shadow it",mod.Name,a.Name)
   }
   for _,b:=range incoming {if strings.EqualFold(a.Name,b.Name){return fmt.Errorf("external assembly %s conflicts with native mod %s",b.Name,mod.Name)}}
  }
 }
 return nil
}

func checkNativeCompatibility(game *GameInfo, inspection *packageInspection) error {
 if !compatibilityEnabled(game) { return nil }
 if len(inspection.Plugins)>0 { return errors.New("a native package cannot contain BepInEx entry points") }
 for _,a:=range inspection.Assemblies {
  name:=strings.ToLower(a.Name)
  if name=="0harmony" || strings.HasPrefix(name,"monomod") || strings.HasPrefix(name,"mono.cecil") || strings.HasPrefix(name,"bepinex") {
   return fmt.Errorf("native package bundles shared runtime library %s; restore native mode before activating it",a.Name)
  }
 }
 external,err:=installedBepPlugins(game);if err!=nil{return err}
 for _,mod:=range external {
  if !mod.Enabled {continue}
  record,err:=readPluginRecord(mod.Path);if err!=nil{return err}
  for _,a:=range inspection.Assemblies {for _,b:=range record.Assemblies {
   if strings.EqualFold(a.Name,b.Name){return fmt.Errorf("native assembly %s conflicts with external plugin %s",a.Name,mod.Name)}
  }}
 }
 return nil
}

func checkInstalledNativeCompatibility(game *GameInfo, path string) error {
 if !compatibilityEnabled(game) {return nil}
 inspection,err:=inspectPackage(path);if err!=nil{return err}
 return checkNativeCompatibility(game,inspection)
}

func isExternalPackageDocument(name string) bool {
 if strings.Contains(name,"/") {return false}
 switch strings.ToLower(name) {
 case "readme", "readme.txt", "readme.md", "license", "license.txt", "license.md", "licence", "licence.txt", "licence.md", "changelog.txt", "changelog.md", "credits.txt", "authors.txt":
  return true
 }
 return false
}

func prepareExternalPayload(staging string, files map[string]string) (string, []ManifestFile, error) {
 // Accept loose plugins or BepInEx/plugins layouts plus known top-level
 // documentation. Preserve resource paths; never deploy bootstrap/core files.
 prefix:=""
 for key:=range files {if strings.HasPrefix(key,"bepinex/plugins/"){prefix="bepinex/plugins/"}}
 payload:=staging
 if prefix!="" {
  for key,path:=range files {
   if strings.HasPrefix(key,prefix) {
    suffix:=key[len(prefix):];payload=path
    for range strings.Split(suffix,"/"){payload=filepath.Dir(payload)}
    break
   }
  }
 }
 var hashes []ManifestFile
 for key,source:=range files {
  path:=source
  if prefix!="" && !strings.HasPrefix(key,prefix) {
   if !isExternalPackageDocument(key) {return "",nil,fmt.Errorf("unsupported file outside BepInEx/plugins: %s",key)}
   path=filepath.Join(payload,"keeperloader-package-docs",filepath.Base(source))
   if _,err:=os.Lstat(path);err==nil{return "",nil,errors.New("package documentation conflicts with a plugin file")}else if !os.IsNotExist(err){return "",nil,err}
   if err:=copyFile(source,path);err!=nil{return "",nil,err}
  } else {
   rel,err:=filepath.Rel(payload,path);if err!=nil{return "",nil,err}
   for _,part:=range strings.Split(strings.ToLower(filepath.ToSlash(rel)),"/") {
    if part=="keeperloader-package-docs" {return "",nil,errors.New("package uses the reserved manager documentation directory")}
   }
  }
  rel,err:=filepath.Rel(payload,path);if err!=nil{return "",nil,err}
  name:=filepath.ToSlash(rel);lower:=strings.ToLower(name)
  for _,part:=range strings.Split(lower,"/"){if part=="core" || part=="patchers" || part=="config" || part=="keepermod.json" || part==bepRecord || part=="keeperloader.activation" || part==modDisabledMarker{return "",nil,fmt.Errorf("reserved package path: %s",name)}}
  ext:=strings.ToLower(filepath.Ext(name));if blockedExtensions[ext] || ext==".zip" || ext==".ini" {return "",nil,fmt.Errorf("unsupported external payload: %s",name)}
  digest,err:=fileSHA256(path);if err!=nil{return "",nil,err}
  hashes=append(hashes,ManifestFile{Path:name,SHA256:digest})
 }
 sort.Slice(hashes,func(i,j int)bool{return hashes[i].Path<hashes[j].Path})
 return payload,hashes,nil
}

func installBepPackage(game *GameInfo, zipPath string, current *InstalledMod, declaredGame bool) (*InstalledMod,string,error) {
 if err:=assertGameStopped(game);err!=nil{return nil,"",err}
 if !compatibilityEnabled(game) || !compatibilityOwned(game) {return nil,"",errors.New("enable BepInEx 5 compatibility first")}
 if !declaredGame {return nil,"",errors.New("confirm that this external mod is published for the selected game; DLL metadata cannot prove game compatibility")}
 if current!=nil {if err:=validateBepSelection(game,current);err!=nil{return nil,"",err}}
 staging,err:=os.MkdirTemp(filepath.Join(game.GameDirectory,"KeeperLoader"),".external-stage-");if err!=nil{return nil,"",err};defer os.RemoveAll(staging)
 files,err:=extractVerifiedArchive(zipPath,staging);if err!=nil{return nil,"",err}
 payload,hashes,err:=prepareExternalPayload(staging,files);if err!=nil{return nil,"",err}
 inspection,err:=inspectPackage(payload);if err!=nil{return nil,"",err}
 plugin,err:=validatePluginMetadata(game,inspection);if err!=nil{return nil,"",err}
 record:=&pluginRecord{Type:bepMode,GameID:game.GameID,Plugin:*plugin,Assemblies:inspection.Assemblies,Files:hashes}
 exclude:="";keepDisabled:=false
 if current!=nil {
  exclude=current.ID;keepDisabled=!current.Enabled
  a,e1:=parseVersion(plugin.Version);b,e2:=parseVersion(current.Version)
  if plugin.ID!=current.ID || e1!=nil || e2!=nil || compareVersion(a,b)<=0{return nil,"",errors.New("update must keep the same plugin GUID and use a newer version")}
 }
 if err=checkPluginConflicts(game,record,exclude);err!=nil{return nil,"",err}
 native,err:=installedMods(game);if err!=nil{return nil,"",err}
 for _,mod:=range native {if strings.EqualFold(mod.ID,plugin.ID) && mod.Mode!=bepMode{return nil,"",errors.New("plugin ID is already used by a native KeeperLoader mod")}}
 if current==nil && (dirExists(filepath.Join(bepManagedRoot(game),plugin.ID)) || dirExists(filepath.Join(bepDisabledRoot(game),plugin.ID))){return nil,"",errors.New("plugin is already installed; use Update selected from ZIP")}
 target:=filepath.Join(bepManagedRoot(game),plugin.ID);if keepDisabled{target=filepath.Join(bepDisabledRoot(game),plugin.ID)}
 data,_:=json.MarshalIndent(record,"","  ");if err=writeAtomic(filepath.Join(payload,bepRecord),data,0644);err!=nil{return nil,"",err}
 backup:=""
 if current!=nil {
  root:=filepath.Join(game.GameDirectory,"KeeperLoader","backup","bepinex");if err=os.MkdirAll(root,0755);err!=nil{return nil,"",err}
  backup=uniqueTimestampPath(root,plugin.ID);if err=os.Rename(current.Path,backup);err!=nil{return nil,"",err}
 }
 if err=os.MkdirAll(filepath.Dir(target),0755);err==nil{err=os.Rename(payload,target)}
 if err!=nil {if backup!=""{_ = os.Rename(backup,current.Path)};return nil,"",err}
 return &InstalledMod{ID:plugin.ID,Name:plugin.Name,Version:plugin.Version,Path:target,Enabled:!keepDisabled,Mode:bepMode},backup,nil
}

func setBepEnabled(game *GameInfo,mod *InstalledMod,enabled bool)(string,error){
 if err:=assertGameStopped(game);err!=nil{return "",err};if err:=validateBepSelection(game,mod);err!=nil{return "",err}
 if mod.Enabled==enabled{return "No change required.",nil}
 target:=filepath.Join(bepDisabledRoot(game),mod.ID)
 if enabled {
  if !compatibilityEnabled(game){return "",errors.New("enable compatibility first")}
  record,err:=readPluginRecord(mod.Path);if err!=nil{return "",err};if err=verifyPluginFiles(mod.Path,record);err!=nil{return "",err}
  if err=checkPluginConflicts(game,record,mod.ID);err!=nil{return "",err};target=filepath.Join(bepManagedRoot(game),mod.ID)
 } else if err:=checkDependentPlugins(game,mod.ID);err!=nil{return "",err}
 if dirExists(target){return "",errors.New("both active and disabled plugin folders exist; refusing to overwrite")}
 if err:=os.MkdirAll(filepath.Dir(target),0755);err!=nil{return "",err};if err:=os.Rename(mod.Path,target);err!=nil{return "",err}
 mod.Path,mod.Enabled=target,enabled
 return "Plugin status changed. Configuration and backups were preserved; restart the game.",nil
}
func checkDependentPlugins(game *GameInfo,id string)error{
 mods,err:=installedBepPlugins(game);if err!=nil{return err}
 for _,mod:=range mods {if !mod.Enabled || mod.ID==id {continue};r,err:=readPluginRecord(mod.Path);if err!=nil{return err};for _,d:=range r.Plugin.Dependencies{if d.Hard && d.ID==id{return fmt.Errorf("disable dependent plugin %s first",mod.Name)}}}
 return nil
}
func verifyPluginFiles(root string,record *pluginRecord)error{
 expected:=map[string]string{bepRecord:""}
 for _,file:=range record.Files {name,err:=normalizedArchivePath(file.Path);if err!=nil{return err};actual,err:=fileSHA256(filepath.Join(root,filepath.FromSlash(name)));if err!=nil || actual!=file.SHA256{return fmt.Errorf("plugin integrity check failed: %s",name)};expected[strings.ToLower(name)]=file.SHA256}
 return filepath.Walk(root,func(path string,info os.FileInfo,err error)error{if err!=nil{return err};if info.Mode()&os.ModeSymlink!=0{return errors.New("plugin contains a symbolic link")};if !info.IsDir(){rel,_:=filepath.Rel(root,path);if _,ok:=expected[strings.ToLower(filepath.ToSlash(rel))];!ok{return fmt.Errorf("undeclared plugin file: %s",rel)}};return nil})
}
func uninstallBep(game *GameInfo,mod *InstalledMod)(string,error){
 if err:=assertGameStopped(game);err!=nil{return "",err};if err:=validateBepSelection(game,mod);err!=nil{return "",err};if err:=checkDependentPlugins(game,mod.ID);err!=nil{return "",err}
 // Preserve unknown plugin-generated data/configuration. Recoverable uninstall.
 root:=filepath.Join(game.GameDirectory,"KeeperLoader","backup","external-uninstalled");if err:=os.MkdirAll(root,0755);err!=nil{return "",err}
 if err:=os.Rename(mod.Path,uniqueTimestampPath(root,mod.ID));err!=nil{return "",err}
 return "External plugin removed from loading and archived. Configuration and backups were preserved; saves were not touched.",nil
}
func restoreBep(game *GameInfo,current *InstalledMod)(*InstalledMod,string,error){
 if err:=assertGameStopped(game);err!=nil{return nil,"",err};if err:=validateBepSelection(game,current);err!=nil{return nil,"",err}
 root:=filepath.Join(game.GameDirectory,"KeeperLoader","backup","bepinex");entries,err:=os.ReadDir(root);if err!=nil{return nil,"",errors.New("no external plugin backup is available")}
 var candidates []string
 for _,entry:=range entries{path:=filepath.Join(root,entry.Name());r,e:=readPluginRecord(path);if e==nil && r.Plugin.ID==current.ID && r.GameID==game.GameID{candidates=append(candidates,path)}}
 sort.Sort(sort.Reverse(sort.StringSlice(candidates)));if len(candidates)==0{return nil,"",errors.New("no previous plugin version is available")}
 previous:=candidates[0];record,err:=readPluginRecord(previous);if err!=nil{return nil,"",err};if err=verifyPluginFiles(previous,record);err!=nil{return nil,"",err}
 if current.Enabled{if err=checkPluginConflicts(game,record,current.ID);err!=nil{return nil,"",err}}
 backup:=uniqueTimestampPath(root,current.ID+"-before-restore");if err=os.Rename(current.Path,backup);err!=nil{return nil,"",err}
 if err=os.Rename(previous,current.Path);err!=nil{_ = os.Rename(backup,current.Path);return nil,"",err}
 return &InstalledMod{ID:current.ID,Name:record.Plugin.Name,Version:record.Plugin.Version,Path:current.Path,Enabled:current.Enabled,Mode:bepMode},backup,nil
}
