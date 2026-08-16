//go:build windows

package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeTestModPackage(t *testing.T, path, id, version, gameID string) {
	t.Helper()
	payload := []byte("managed-dll-placeholder-" + id + "-" + version)
	digest := sha256.Sum256(payload)
	manifest := ModManifest{
		ID: id, Name: "Test Mod", Version: version,
		MinimumKeeperLoaderVersion: loaderVersion,
		UnityBackend:               "Mono", SupportedGames: []string{gameID},
		Files: []ManifestFile{{Path: "Test.Mod.dll", SHA256: hex.EncodeToString(digest[:])}},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(output)
	manifestWriter, err := archive.Create("keepermod.json")
	if err == nil {
		_, err = manifestWriter.Write(manifestData)
	}
	if err == nil {
		var payloadWriter interface{ Write([]byte) (int, error) }
		payloadWriter, err = archive.Create("Test.Mod.dll")
		if err == nil {
			_, err = payloadWriter.Write(payload)
		}
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestModUpdatePreservesDataRejectsWrongPackageAndRestores(t *testing.T) {
	root := t.TempDir()
	game := &GameInfo{
		GameDirectory: root, ExecutableName: "KeeperLoader-Test-Process.exe",
		GameID: "test-game", Backend: "Mono", Architecture: "x64", Supported: true,
	}
	loader := filepath.Join(root, "KeeperLoader")
	if err := os.MkdirAll(filepath.Join(loader, "state"), 0755); err != nil {
		t.Fatal(err)
	}
	record, _ := json.Marshal(map[string]string{"gameId": game.GameID, "keeperLoaderVersion": loaderVersion})
	if err := os.WriteFile(filepath.Join(loader, "state", "game.json"), record, 0644); err != nil {
		t.Fatal(err)
	}
	versionOne := filepath.Join(t.TempDir(), "test-mod-1.0.0.zip")
	writeTestModPackage(t, versionOne, "example.test-mod", "1.0.0", game.GameID)
	installed, _, err := installModPackage(game, versionOne)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(loader, "config", installed.ID+".cfg")
	statePath := filepath.Join(loader, "state", installed.ID, "state.json")
	if err = os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, []byte("setting=true"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(statePath, []byte("persistent-state"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = setModEnabled(game, installed, false); err != nil {
		t.Fatal(err)
	}
	if installed.Enabled || !fileExists(filepath.Join(installed.Path, modDisabledMarker)) {
		t.Fatal("mod was not disabled")
	}
	listed, listErr := installedMods(game)
	if listErr != nil || len(listed) != 1 || listed[0].Enabled {
		t.Fatalf("disabled mod was not listed correctly: %#v, %v", listed, listErr)
	}
	versionTwo := filepath.Join(t.TempDir(), "test-mod-1.1.0.zip")
	writeTestModPackage(t, versionTwo, installed.ID, "1.1.0", game.GameID)
	updated, backup, err := updateModPackage(game, installed, versionTwo)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != "1.1.0" || backup == "" || !dirExists(backup) {
		t.Fatalf("unexpected update result: version=%s backup=%s", updated.Version, backup)
	}
	if updated.Enabled || !fileExists(filepath.Join(updated.Path, modDisabledMarker)) {
		t.Fatal("mod update did not preserve disabled status")
	}
	if data, readErr := os.ReadFile(configPath); readErr != nil || string(data) != "setting=true" {
		t.Fatalf("configuration was not preserved: %q, %v", string(data), readErr)
	}
	if data, readErr := os.ReadFile(statePath); readErr != nil || string(data) != "persistent-state" {
		t.Fatalf("state was not preserved: %q, %v", string(data), readErr)
	}
	if _, _, err = updateModPackage(game, updated, versionTwo); err == nil {
		t.Fatal("same-version update was accepted")
	}
	wrongPackage := filepath.Join(t.TempDir(), "wrong-mod-2.0.0.zip")
	writeTestModPackage(t, wrongPackage, "example.wrong-mod", "2.0.0", game.GameID)
	if _, _, err = updateModPackage(game, updated, wrongPackage); err == nil {
		t.Fatal("wrong-mod update was accepted")
	}
	restored, _, err := restorePreviousMod(game, updated)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version != "1.0.0" {
		t.Fatalf("expected restored version 1.0.0, got %s", restored.Version)
	}
	if restored.Enabled || !fileExists(filepath.Join(restored.Path, modDisabledMarker)) {
		t.Fatal("mod restore did not preserve disabled status")
	}
	if data, readErr := os.ReadFile(configPath); readErr != nil || string(data) != "setting=true" {
		t.Fatalf("configuration was not preserved through rollback: %q, %v", string(data), readErr)
	}
	if data, readErr := os.ReadFile(statePath); readErr != nil || string(data) != "persistent-state" {
		t.Fatalf("state was not preserved through rollback: %q, %v", string(data), readErr)
	}
	if _, err = setModEnabled(game, restored, true); err != nil {
		t.Fatal(err)
	}
	if !restored.Enabled || fileExists(filepath.Join(restored.Path, modDisabledMarker)) {
		t.Fatal("mod was not enabled again")
	}
	if data, readErr := os.ReadFile(configPath); readErr != nil || string(data) != "setting=true" {
		t.Fatalf("configuration was changed by disable/enable: %q, %v", string(data), readErr)
	}
	if data, readErr := os.ReadFile(statePath); readErr != nil || string(data) != "persistent-state" {
		t.Fatalf("state was changed by disable/enable: %q, %v", string(data), readErr)
	}
	savePath := filepath.Join(root, "game-save.dat")
	if err = os.WriteFile(savePath, []byte("save data"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = uninstallMod(game, restored); err != nil {
		t.Fatal(err)
	}
	for _, removedPath := range []string{restored.Path, configPath, filepath.Dir(statePath)} {
		if fileExists(removedPath) || dirExists(removedPath) {
			t.Fatalf("uninstall left managed data behind: %s", removedPath)
		}
	}
	for _, backupRoot := range []string{
		filepath.Join(loader, "backup", "mods"),
		filepath.Join(loader, "backup", "uninstalled"),
	} {
		entries, readErr := os.ReadDir(backupRoot)
		if readErr != nil && !os.IsNotExist(readErr) {
			t.Fatal(readErr)
		}
		for _, entry := range entries {
			if len(entry.Name()) >= len(restored.ID)+1 && entry.Name()[:len(restored.ID)+1] == restored.ID+"-" {
				t.Fatalf("uninstall left a matching mod backup behind: %s", entry.Name())
			}
		}
	}
	if data, readErr := os.ReadFile(savePath); readErr != nil || string(data) != "save data" {
		t.Fatalf("game save was changed by uninstall: %q, %v", string(data), readErr)
	}
}
