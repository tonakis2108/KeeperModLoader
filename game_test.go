//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSteamManifestLookupAndRunURI(t *testing.T) {
	root := t.TempDir()
	steamApps := filepath.Join(root, "steamapps")
	gameDirectory := filepath.Join(steamApps, "common", "Graveyard Keeper")
	if err := os.MkdirAll(gameDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `"AppState"
{
    "appid"        "599140"
    "installdir"  "Graveyard Keeper"
}`
	if err := os.WriteFile(filepath.Join(steamApps, "appmanifest_599140.acf"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if appID := steamAppIDForGameDirectoryFromRoots(gameDirectory, []string{root}); appID != "599140" {
		t.Fatalf("expected Steam App ID 599140, got %q", appID)
	}
	uri, err := steamRunURI("599140")
	if err != nil || uri != "steam://run/599140" {
		t.Fatalf("unexpected Steam URI %q, error=%v", uri, err)
	}
	if _, err = steamRunURI("599140&unsafe=true"); err == nil {
		t.Fatal("unsafe Steam App ID was accepted")
	}
	if appID := steamAppIDForGameDirectoryFromRoots(filepath.Join(root, "steamapps", "common", "Missing Game"), []string{root}); appID != "" {
		t.Fatalf("unexpected Steam App ID for an installation without a manifest: %q", appID)
	}
}

func TestMergeGamesKeepsSteamMetadata(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "Game")
	existing := &GameInfo{GameDirectory: directory, ProcessName: "Game"}
	detected := &GameInfo{GameDirectory: directory, ProcessName: "Game", SteamAppID: "12345"}
	merged := mergeGames([]*GameInfo{existing}, []*GameInfo{detected})
	if len(merged) != 1 || merged[0].SteamAppID != "12345" {
		t.Fatalf("Steam metadata was not merged: %#v", merged)
	}
}
