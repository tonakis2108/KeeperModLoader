//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestActivateCoreUpdateRollsBack(t *testing.T) {
	loader := filepath.Join(t.TempDir(), "KeeperLoader")
	core := filepath.Join(loader, "core")
	if err := os.MkdirAll(core, 0755); err != nil {
		t.Fatal(err)
	}
	oldAPI := filepath.Join(core, "KeeperLoader.API.dll")
	if err := os.WriteFile(oldAPI, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	built := filepath.Join(t.TempDir(), "KeeperLoader.API.dll")
	if err := os.WriteFile(built, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	backup, rollback, err := activateCoreUpdate(loader, [][2]string{{built, oldAPI}})
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" || !dirExists(backup) {
		t.Fatal("previous core was not backed up")
	}
	data, err := os.ReadFile(oldAPI)
	if err != nil || string(data) != "new" {
		t.Fatalf("new core was not activated: %q, %v", string(data), err)
	}
	rollback()
	data, err = os.ReadFile(oldAPI)
	if err != nil || string(data) != "old" {
		t.Fatalf("rollback did not restore previous core: %q, %v", string(data), err)
	}
}

func TestDisableLoaderRemovesManagedFilesAndRestoresOriginalBootstrap(t *testing.T) {
	root := t.TempDir()
	game := &GameInfo{
		GameDirectory:  root,
		ExecutableName: "KeeperLoader-Removal-Test.exe",
		ProcessName:    "KeeperLoader Removal Test",
	}
	loader := filepath.Join(root, "KeeperLoader")
	backup := filepath.Join(loader, "backup", "original", "initial")
	if err := os.MkdirAll(filepath.Join(loader, "state"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{
		filepath.Join(loader, "core"),
		filepath.Join(loader, "mods", "example.mod"),
		filepath.Join(loader, "config"),
		filepath.Join(loader, "logs"),
		backup,
	} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(backup, "winhttp.dll"), []byte("original proxy"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "doorstop_config.ini"), []byte("original config"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loader, "state", "last-backup.txt"), []byte(backup+"\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "winhttp.dll"), []byte("keeperloader proxy"), 0644); err != nil {
		t.Fatal(err)
	}
	config := "[General]\r\ntarget_assembly=KeeperLoader\\core\\KeeperLoader.Bootstrap.dll\r\n"
	if err := os.WriteFile(filepath.Join(root, "doorstop_config.ini"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "winhttp.keeperloader-disabled.dll"), []byte("disabled proxy"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loader, "mods", "example.mod", "mod.dll"), []byte("mod"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loader, "config", "example.mod.cfg"), []byte("setting=true"), 0644); err != nil {
		t.Fatal(err)
	}
	save := filepath.Join(root, "game-save.dat")
	if err := os.WriteFile(save, []byte("save data"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := disableLoader(game); err != nil {
		t.Fatal(err)
	}
	if dirExists(loader) {
		t.Fatal("KeeperLoader directory was not removed")
	}
	if fileExists(filepath.Join(root, "winhttp.keeperloader-disabled.dll")) {
		t.Fatal("disabled KeeperLoader proxy was not removed")
	}
	if data, err := os.ReadFile(filepath.Join(root, "winhttp.dll")); err != nil || string(data) != "original proxy" {
		t.Fatalf("original proxy was not restored: %q, %v", string(data), err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "doorstop_config.ini")); err != nil || string(data) != "original config" {
		t.Fatalf("original configuration was not restored: %q, %v", string(data), err)
	}
	if data, err := os.ReadFile(save); err != nil || string(data) != "save data" {
		t.Fatalf("game save was changed: %q, %v", string(data), err)
	}
}
