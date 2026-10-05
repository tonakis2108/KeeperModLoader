//go:build windows

package main

import (
 "encoding/json"
 "errors"
 "strings"
 "os"
 "path/filepath"
 "testing"
)

func pluginTestGame(t *testing.T) *GameInfo {
 t.Helper()
 return &GameInfo{GameDirectory:t.TempDir(),GameID:graveyardKeeperGameID,ExecutableName:"Graveyard Keeper.exe",Backend:"Mono",Architecture:"x64",Supported:true}
}
func writePluginTestRecord(t *testing.T,game *GameInfo,id string,enabled bool,dependencies []pluginDependency) *InstalledMod {
 t.Helper()
 root:=bepDisabledRoot(game);if enabled{root=bepManagedRoot(game)}
 path:=filepath.Join(root,id);if err:=os.MkdirAll(path,0755);err!=nil{t.Fatal(err)}
 record:=pluginRecord{Type:bepMode,GameID:game.GameID,Plugin:pluginMetadata{ID:id,Name:id,Version:"1.0.0",Dependencies:dependencies}}
 data,_:=json.Marshal(record);if err:=os.WriteFile(filepath.Join(path,bepRecord),data,0644);err!=nil{t.Fatal(err)}
 return &InstalledMod{ID:id,Name:id,Version:"1.0.0",Path:path,Enabled:enabled,Mode:bepMode}
}

func TestBepMetadataClassification(t *testing.T) {
 game:=pluginTestGame(t)
 good:=pluginMetadata{ID:"author.plugin",Name:"Plugin",Version:"1.0.0",Processes:[]string{"Graveyard Keeper.exe"}}
 cases:=[]struct{name string;report packageInspection;reject bool}{
  {"valid",packageInspection{Plugins:[]pluginMetadata{good},Assemblies:[]pluginAssembly{{Name:"Author.Plugin"}}},false},
  {"mixed",packageInspection{Native:true,Plugins:[]pluginMetadata{good}},true},
  {"unknown",packageInspection{},true},
  {"multiple entry points",packageInspection{Plugins:[]pluginMetadata{good,good}},true},
  {"shared harmony",packageInspection{Plugins:[]pluginMetadata{good},Assemblies:[]pluginAssembly{{Name:"0Harmony"}}},true},
  {"game assembly",packageInspection{Plugins:[]pluginMetadata{good},Assemblies:[]pluginAssembly{{Name:"Assembly-CSharp"}}},true},
  {"duplicate assemblies",packageInspection{Plugins:[]pluginMetadata{good},Assemblies:[]pluginAssembly{{Name:"Private"},{Name:"private"}}},true},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){_,err:=validatePluginMetadata(game,&tc.report);if (err!=nil)!=tc.reject{t.Fatalf("reject=%v, error=%v",tc.reject,err)}})}
 wrong:=good;wrong.Processes=[]string{"OtherGame.exe"};if _,err:=validatePluginMetadata(game,&packageInspection{Plugins:[]pluginMetadata{wrong}});err==nil{t.Fatal("wrong game process accepted")}
 reserved:=good;reserved.ID=bepHostID;if _,err:=validatePluginMetadata(game,&packageInspection{Plugins:[]pluginMetadata{reserved}});err==nil{t.Fatal("reserved host GUID accepted")}
 for _,id:=range []string{".","..","CON","AUX",".hidden","keeperloader.nativehost"}{if validPluginID(id){t.Fatalf("unsafe plugin ID accepted: %s",id)}}
}
func TestExternalDirectoriesAndConfigurationStaySeparate(t *testing.T) {
 game:=pluginTestGame(t);mod:=writePluginTestRecord(t,game,"author.example",true,nil)
 config:=filepath.Join(bepRoot(game),"config","author.example.cfg");if err:=writeAtomic(config,[]byte("keep=yes"),0644);err!=nil{t.Fatal(err)}
 if _,err:=setModEnabled(game,mod,false);err!=nil{t.Fatal(err)}
 if dirExists(filepath.Join(bepManagedRoot(game),mod.ID)) || !dirExists(filepath.Join(bepDisabledRoot(game),mod.ID)){t.Fatal("disabled package remains inside plugin discovery")}
 if _,err:=setModEnabled(game,mod,true);err==nil{t.Fatal("external enable allowed while runtime inactive")}
 marker:=filepath.Join(game.GameDirectory,"KeeperLoader","state",bepMarker);if err:=writeAtomic(marker,[]byte("enabled"),0644);err!=nil{t.Fatal(err)}
 if _,err:=setModEnabled(game,mod,true);err!=nil{t.Fatal(err)}
 if _,err:=restoreNativeMode(game);err!=nil{t.Fatal(err)}
 if !dirExists(mod.Path) || !fileExists(config){t.Fatal("native mode destroyed external package/config")}
 if _,err:=uninstallMod(game,mod);err!=nil{t.Fatal(err)}
 if dirExists(mod.Path) || !fileExists(config){t.Fatal("external uninstall failed to preserve config")}
}
func TestMissingDependencyAndDependentDisableAreBlocked(t *testing.T){
 game:=pluginTestGame(t)
 incoming:=&pluginRecord{Plugin:pluginMetadata{ID:"dependent",Dependencies:[]pluginDependency{{ID:"required",MinimumVersion:"1.0.0",Hard:true}}}}
 if err:=checkPluginConflicts(game,incoming,"");err==nil{t.Fatal("missing dependency accepted")}
 required:=writePluginTestRecord(t,game,"required",true,nil)
 if err:=checkPluginConflicts(game,incoming,"");err!=nil{t.Fatal(err)}
 writePluginTestRecord(t,game,"dependent",true,incoming.Plugin.Dependencies)
 if _,err:=setModEnabled(game,required,false);err==nil{t.Fatal("enabled dependent was broken")}
 if _,err:=uninstallMod(game,required);err==nil{t.Fatal("dependency uninstall broke enabled dependent")}
}
func TestPluginTamperAndPathEscapeRejected(t *testing.T){
 game:=pluginTestGame(t);mod:=writePluginTestRecord(t,game,"author.plugin",false,nil)
 if err:=os.WriteFile(filepath.Join(mod.Path,"injected.dll"),[]byte("payload"),0644);err!=nil{t.Fatal(err)}
 record,err:=readPluginRecord(mod.Path);if err!=nil{t.Fatal(err)}
 if err=verifyPluginFiles(mod.Path,record);err==nil{t.Fatal("undeclared DLL accepted")}
 mod.Path=game.GameDirectory;if err=validateBepSelection(game,mod);err==nil{t.Fatal("escaped managed folder accepted")}
 for _,path:=range []string{"../mod.dll","/mod.dll","C:/mod.dll","CON.dll","file.dll.","folder/NUL.txt","mod.dll:stream"}{if _,err:=normalizedArchivePath(path);err==nil{t.Fatalf("unsafe Windows path accepted: %s",path)}}
}
func TestNativeAndExternalRegistryUnion(t *testing.T){
 game:=pluginTestGame(t);writePluginTestRecord(t,game,"external.example",false,nil)
 mods,err:=installedMods(game);if err!=nil{t.Fatal(err)}
 if len(mods)!=1 || mods[0].Mode!=bepMode || mods[0].Enabled{t.Fatalf("incorrect unified registry: %#v",mods)}
}

func TestNativeSharedLibrariesOnlyRejectedInCompatibilityMode(t *testing.T){
 game:=pluginTestGame(t)
 report:=&packageInspection{Assemblies:[]pluginAssembly{{Name:"0Harmony"}}}
 if err:=checkNativeCompatibility(game,report);err!=nil{t.Fatal("native-only mode changed:",err)}
 if err:=writeAtomic(filepath.Join(game.GameDirectory,"KeeperLoader","state",bepMarker),[]byte("enabled"),0644);err!=nil{t.Fatal(err)}
 if err:=checkNativeCompatibility(game,report);err==nil{t.Fatal("shared native runtime library accepted")}
 external:=writePluginTestRecord(t,game,"external.example",true,nil)
 record,err:=readPluginRecord(external.Path);if err!=nil{t.Fatal(err)}
 record.Assemblies=[]pluginAssembly{{Name:"Private.Library"}}
 data,_:=json.Marshal(record);if err=os.WriteFile(filepath.Join(external.Path,bepRecord),data,0644);err!=nil{t.Fatal(err)}
 report.Assemblies=[]pluginAssembly{{Name:"private.library"}}
 if err=checkNativeCompatibility(game,report);err==nil{t.Fatal("native/external assembly collision accepted")}
 report.Assemblies=[]pluginAssembly{{Name:"Native.Unique"}}
 if err=checkNativeCompatibility(game,report);err!=nil{t.Fatal(err)}
}

func TestMoveFurnitureArchiveLayoutAndReadme(t *testing.T) {
 // Matches the supplied MoveFurniture ZIP: Windows directory entry with
 // no directory attributes, a nested plugin DLL, and root README.txt.
 archive:=writeArchiveLayoutFixture(t,[]string{`BepInEx\plugins\`, `BepInEx\plugins\GK_MoveFurniture\GK_MoveFurniture.dll`, "README.txt"})
 stage:=t.TempDir()
 files,err:=extractVerifiedArchive(archive,stage);if err!=nil{t.Fatal(err)}
 pluginPath:=files["bepinex/plugins/gk_movefurniture/gk_movefurniture.dll"]
 before,err:=fileSHA256(pluginPath);if err!=nil{t.Fatal(err)}
 payload,hashes,err:=prepareExternalPayload(stage,files);if err!=nil{t.Fatal(err)}
 if payload!=filepath.Join(stage,"BepInEx","plugins") || len(hashes)!=2{t.Fatalf("incorrect plugin layout: %s, %#v",payload,hashes)}
 if !fileExists(filepath.Join(payload,"GK_MoveFurniture","GK_MoveFurniture.dll")) || !fileExists(filepath.Join(payload,"keeperloader-package-docs","README.txt")){t.Fatal("plugin or publisher documentation lost")}
 after,err:=fileSHA256(pluginPath);if err!=nil || before!=after{t.Fatal("plugin DLL bytes changed")}
}

func TestRootDocumentationDoesNotAdmitRuntimeFiles(t *testing.T) {
 for _,name:=range []string{"winhttp.dll","doorstop_config.ini","install.cmd","BepInEx/core/BepInEx.dll","README.exe","docs/README.txt"} {
  archive:=writeArchiveLayoutFixture(t,[]string{"BepInEx/plugins/Plugin.dll",name})
  stage:=t.TempDir();files,err:=extractVerifiedArchive(archive,stage);if err!=nil{t.Fatal(err)}
  if _,_,err=prepareExternalPayload(stage,files);err==nil{t.Fatalf("unexpected root payload accepted: %s",name)}
 }
}


func compatibilityRepairFixture(t *testing.T) (*GameInfo,string) {
 t.Helper()
 game:=pluginTestGame(t)
 source:=t.TempDir()
 files:=map[string]string{
  "core/BepInEx.dll":"trusted official core",
  "core/BepInEx.Preloader.dll":"trusted official preloader",
  "plugins/KeeperLoaderManaged/keeperloader.nativehost/KeeperLoader.NativeHost.dll":"new host build",
  "licenses/BepInEx-MIT.txt":"retained licence",
  "KEEPERLOADER-COMPATIBILITY-NOTICE.txt":"new notice",
  "keeperloader.runtime-sha256":"trusted runtime hashes",
 }
 for rel,data:=range files{if err:=writeAtomic(filepath.Join(source,filepath.FromSlash(rel)),[]byte(data),0644);err!=nil{t.Fatal(err)}}
 if err:=copyDirectory(source,bepRoot(game));err!=nil{t.Fatal(err)}
 if err:=writeAtomic(filepath.Join(bepRoot(game),".keeperloader-owned"),[]byte("owned"),0644);err!=nil{t.Fatal(err)}
 if err:=writeAtomic(filepath.Join(game.GameDirectory,"KeeperLoader","state",bepMarker),[]byte("enabled"),0644);err!=nil{t.Fatal(err)}
 return game,source
}

func TestCompatibilityRepairPreservesPackagesAndSettings(t *testing.T) {
 game,source:=compatibilityRepairFixture(t)
 enabled:=writePluginTestRecord(t,game,"external.enabled",true,nil)
 disabled:=writePluginTestRecord(t,game,"external.disabled",false,nil)
 preserved:=[]string{
  filepath.Join(enabled.Path,bepRecord),filepath.Join(disabled.Path,bepRecord),
  filepath.Join(bepRoot(game),"config","external.enabled.cfg"),
  filepath.Join(game.GameDirectory,"KeeperLoader","mods","native.example","Native.dll"),
  filepath.Join(game.GameDirectory,"KeeperLoader","mods","native.example",modDisabledMarker),
  filepath.Join(game.GameDirectory,"KeeperLoader","config","native.example","settings.json"),
 }
 before:=map[string]string{}
 for _,path:=range preserved{
  if !fileExists(path){if err:=writeAtomic(path,[]byte("user data stays byte-for-byte"),0644);err!=nil{t.Fatal(err)}}
  hash,err:=fileSHA256(path);if err!=nil{t.Fatal(err)};before[path]=hash
 }
 notice:=filepath.Join(bepRoot(game),"KEEPERLOADER-COMPATIBILITY-NOTICE.txt")
 host:=filepath.Join(bepManagedRoot(game),bepHostID,"KeeperLoader.NativeHost.dll")
 if err:=os.Remove(notice);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(host,[]byte("previous legitimate host build"),0644);err!=nil{t.Fatal(err)}
 if err:=verifyCompatibilityTree(game,source);err==nil{t.Fatal("outdated runtime incorrectly verified")}
 if err:=refreshOwnedCompatibility(game,source);err!=nil{t.Fatal(err)}
 if err:=verifyCompatibilityTree(game,source);err!=nil{t.Fatal(err)}
 // A later manager can change notices independently of the core runtime.
 if err:=os.WriteFile(filepath.Join(source,"KEEPERLOADER-COMPATIBILITY-NOTICE.txt"),[]byte("updated documentation"),0644);err!=nil{t.Fatal(err)}
 if err:=refreshOwnedCompatibility(game,source);err!=nil{t.Fatal(err)}
 for path,wanted:=range before{actual,err:=fileSHA256(path);if err!=nil||wanted!=actual{t.Fatalf("user file changed: %s: %v",path,err)}}
 if !compatibilityEnabled(game){t.Fatal("repair changed activation state")}
}

func TestCompatibilityRepairRollsBackOnRenameFailure(t *testing.T) {
 game,source:=compatibilityRepairFixture(t)
 core:=filepath.Join(bepRoot(game),"core","BepInEx.dll")
 host:=filepath.Join(bepManagedRoot(game),bepHostID,"KeeperLoader.NativeHost.dll")
 for _,path:=range []string{core,host}{if err:=os.WriteFile(path,[]byte("previous runtime"),0644);err!=nil{t.Fatal(err)}}
 beforeCore,_:=fileSHA256(core);beforeHost,_:=fileSHA256(host)
 calls:=0
 rename:=func(from,to string)error{calls++;if calls==4{return errors.New("simulated locked host")};return os.Rename(from,to)}
 err:=refreshOwnedCompatibilityWithRename(game,source,rename)
 if err==nil||!strings.Contains(err.Error(),"previous runtime restored"){t.Fatalf("expected restored transaction, got %v",err)}
 afterCore,e1:=fileSHA256(core);afterHost,e2:=fileSHA256(host)
 if e1!=nil||e2!=nil||beforeCore!=afterCore||beforeHost!=afterHost{t.Fatal("rollback did not restore previous runtime")}
 if !compatibilityEnabled(game){t.Fatal("successful rollback changed activation state")}
}

func TestCompatibilityRepairRejectsUnownedCode(t *testing.T) {
 for _,rel:=range []string{"core/Injected.dll","plugins/Other.dll","plugins/KeeperLoaderManaged/unknown/Plugin.dll","plugins/KeeperLoaderManaged/keeperloader.nativehost/Extra.dll","patchers/Patch.txt"}{
  t.Run(rel,func(t *testing.T){
   game,source:=compatibilityRepairFixture(t)
   notice:=filepath.Join(bepRoot(game),"KEEPERLOADER-COMPATIBILITY-NOTICE.txt")
   if err:=os.WriteFile(notice,[]byte("old notice"),0644);err!=nil{t.Fatal(err)}
   before,_:=fileSHA256(notice)
   if err:=writeAtomic(filepath.Join(bepRoot(game),filepath.FromSlash(rel)),[]byte("unowned"),0644);err!=nil{t.Fatal(err)}
   if err:=refreshOwnedCompatibility(game,source);err==nil{t.Fatal("unowned code adopted by repair")}
   after,_:=fileSHA256(notice);if before!=after{t.Fatal("failed audit modified installed runtime")}
  })
 }
 game,source:=compatibilityRepairFixture(t)
 if err:=os.Remove(filepath.Join(bepRoot(game),".keeperloader-owned"));err!=nil{t.Fatal(err)}
 if err:=refreshOwnedCompatibility(game,source);err==nil{t.Fatal("unowned runtime adopted")}
}


func TestCompatibilityRepairRetainsBackupWhenRollbackFails(t *testing.T) {
 game,source:=compatibilityRepairFixture(t)
 host:=filepath.Join(bepManagedRoot(game),bepHostID,"KeeperLoader.NativeHost.dll")
 if err:=os.WriteFile(host,[]byte("old host to recover"),0644);err!=nil{t.Fatal(err)}
 calls:=0
 rename:=func(from,to string)error{calls++;if calls==4||calls==5{return errors.New("simulated locked host")};return os.Rename(from,to)}
 err:=refreshOwnedCompatibilityWithRename(game,source,rename)
 if err==nil||!strings.Contains(err.Error(),"backups retained"){t.Fatalf("rollback failure was hidden: %v",err)}
 if compatibilityEnabled(game){t.Fatal("partially refreshed runtime left active")}
 matches,e:=filepath.Glob(filepath.Join(game.GameDirectory,"KeeperLoader",".compatibility-refresh-*","old-1","KeeperLoader.NativeHost.dll"))
 if e!=nil||len(matches)!=1{t.Fatalf("recoverable old host backup missing: %v %v",matches,e)}
 data,e:=os.ReadFile(matches[0]);if e!=nil||string(data)!="old host to recover"{t.Fatal("old host backup bytes lost")}
}
