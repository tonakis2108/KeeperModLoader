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
