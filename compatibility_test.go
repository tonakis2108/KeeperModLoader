//go:build windows

package main

import (
 "encoding/json"
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
