//go:build windows

package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestModPackage(t *testing.T, path, id, version, gameID string) {
	writeTestModPackageWithEntryMode(t, path, id, version, gameID, "")
}

func writeTestModPackageWithEntryMode(t *testing.T, path, id, version, gameID, entryMode string) {
	t.Helper()
	payload := []byte("managed-dll-placeholder-" + id + "-" + version)
	digest := sha256.Sum256(payload)
	manifest := ModManifest{
		ID: id, Name: "Test Mod", Version: version, EntryMode: entryMode,
		// Packages built for 0.6.1 must remain installable without modification.
		MinimumKeeperLoaderVersion: "0.6.1",
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

func writeLegacyBuildModPackage(t *testing.T, path, id, version, gameID string, includeCompiledDLL bool) {
	t.Helper()
	sourceName := "src/LegacyMod.cs"
	outputName := "Legacy.Mod.dll"
	source := []byte("public sealed class LegacyMod { }")
	sourceDigest := sha256.Sum256(source)
	manifest := ModManifest{
		ID: id, Name: "Legacy Build Mod", Version: version,
		MinimumKeeperLoaderVersion: "0.6.1", UnityBackend: "Mono", SupportedGames: []string{gameID},
		Files: []ManifestFile{{Path: sourceName, SHA256: hex.EncodeToString(sourceDigest[:])}},
		Build: &BuildSpec{Sources: []string{sourceName}, Output: outputName},
	}
	payloads := map[string][]byte{sourceName: source}
	if includeCompiledDLL {
		compiled := []byte("precompiled-managed-dll-placeholder-" + id + "-" + version)
		compiledDigest := sha256.Sum256(compiled)
		manifest.Files = append(manifest.Files, ManifestFile{Path: outputName, SHA256: hex.EncodeToString(compiledDigest[:])})
		payloads[outputName] = compiled
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
	for name, payload := range payloads {
		if err != nil {
			break
		}
		var writer interface{ Write([]byte) (int, error) }
		writer, err = archive.Create(name)
		if err == nil {
			_, err = writer.Write(payload)
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

func TestLegacySourcePackagesRequirePublisherCompilationButHybridUpdatesRemainCompatible(t *testing.T) {
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

	legacySourceOnly := filepath.Join(t.TempDir(), "legacy-source-only.zip")
	writeLegacyBuildModPackage(t, legacySourceOnly, "example.legacy", "1.0.0", game.GameID, false)
	if _, _, err := installModPackage(game, legacySourceOnly); err == nil || !strings.Contains(err.Error(), "legacy source-only package detected") {
		t.Fatalf("source-only legacy package did not receive the migration error: %v", err)
	}

	installedPackage := filepath.Join(t.TempDir(), "legacy-precompiled-1.0.0.zip")
	writeLegacyBuildModPackage(t, installedPackage, "example.legacy", "1.0.0", game.GameID, true)
	installed, _, err := installModPackage(game, installedPackage)
	if err != nil {
		t.Fatalf("precompiled package retaining legacy metadata was rejected: %v", err)
	}
	if _, err = setModEnabled(game, installed, false); err != nil {
		t.Fatal(err)
	}

	updatePackage := filepath.Join(t.TempDir(), "legacy-precompiled-1.1.0.zip")
	writeLegacyBuildModPackage(t, updatePackage, installed.ID, "1.1.0", game.GameID, true)
	updated, backup, err := updateModPackage(game, installed, updatePackage)
	if err != nil {
		t.Fatalf("legacy-format precompiled update was rejected: %v", err)
	}
	if updated.Version != "1.1.0" || updated.Enabled || backup == "" || !dirExists(backup) {
		t.Fatalf("legacy update did not preserve normal update behavior: %#v, backup=%q", updated, backup)
	}
}

func TestNativeManifestCompatibilityAndLegacyExternalMigration(t *testing.T) {
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

	legacyCompatible := filepath.Join(t.TempDir(), "native-omitted-entry-mode.zip")
	writeTestModPackage(t, legacyCompatible, "example.native-omitted", "1.0.0", game.GameID)
	installed, _, err := installModPackage(game, legacyCompatible)
	if err != nil {
		t.Fatal(err)
	}
	if installed.ID != "example.native-omitted" || installed.Mode != "native" || !installed.Enabled {
		t.Fatalf("older native package compatibility changed: %#v", installed)
	}

	explicitNative := filepath.Join(t.TempDir(), "native-explicit-entry-mode.zip")
	writeTestModPackageWithEntryMode(t, explicitNative, "example.native-explicit", "1.0.0", game.GameID, "native")
	installedExplicit, _, err := installModPackage(game, explicitNative)
	if err != nil {
		t.Fatal(err)
	}
	if installedExplicit.Mode != "native" || !installedExplicit.Enabled {
		t.Fatalf("explicit native entry mode was rejected: %#v", installedExplicit)
	}

	externalPackage := filepath.Join(t.TempDir(), "retired-external-entry-mode.zip")
	writeTestModPackageWithEntryMode(t, externalPackage, "external.retired", "1.0.0", game.GameID, legacyExternalPluginMode)
	if _, _, err = installModPackage(game, externalPackage); err == nil {
		t.Fatal("retired external entry mode was accepted")
	}

	legacyID := "external.legacy-package"
	legacyPath := filepath.Join(loader, "mods", legacyID)
	if err = os.MkdirAll(legacyPath, 0755); err != nil {
		t.Fatal(err)
	}
	legacyManifest, _ := json.Marshal(ModManifest{
		ID: legacyID, Name: "Legacy External Package", Version: "1.0.0",
		EntryMode: legacyExternalPluginMode,
	})
	if err = os.WriteFile(filepath.Join(legacyPath, "keepermod.json"), legacyManifest, 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(legacyPath, "External.Plugin.dll"), []byte("legacy"), 0644); err != nil {
		t.Fatal(err)
	}
	listed, err := installedMods(game)
	if err != nil {
		t.Fatal(err)
	}
	var legacy *InstalledMod
	for _, candidate := range listed {
		if candidate.ID == legacyID {
			legacy = candidate
			break
		}
	}
	if legacy == nil || legacy.Enabled || legacy.Mode != legacyExternalPluginMode || legacy.Status != "inactive" {
		t.Fatalf("legacy external package was not retained as inactive: %#v", legacy)
	}
	if _, err = setModEnabled(game, legacy, true); err == nil {
		t.Fatal("legacy external package could be re-enabled")
	}
	if _, err = uninstallMod(game, legacy); err != nil {
		t.Fatal(err)
	}
	if dirExists(legacyPath) {
		t.Fatal("legacy external package could not be uninstalled")
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
