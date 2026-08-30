package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type managerSettings struct {
	GameDirectories []string `json:"gameDirectories"`
}

func managerSettingsPath() string {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		root = executableDirectory()
	}
	return filepath.Join(root, "KeeperLoader", "manager.json")
}

func loadRememberedGames() ([]*GameInfo, error) {
	data, err := os.ReadFile(managerSettingsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var settings managerSettings
	if err = json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	var games []*GameInfo
	seen := map[string]bool{}
	for _, directory := range settings.GameDirectories {
		game, detectErr := detectUnityGame(directory)
		if detectErr != nil || !game.Supported || game.GameID != graveyardKeeperGameID {
			continue
		}
		game.SteamAppID = steamAppIDForGameDirectory(game.GameDirectory)
		key := strings.ToLower(game.GameDirectory)
		if !seen[key] {
			seen[key] = true
			games = append(games, game)
		}
	}
	sort.Slice(games, func(i, j int) bool {
		return strings.ToLower(games[i].ProcessName) < strings.ToLower(games[j].ProcessName)
	})
	return games, nil
}

func saveRememberedGames(games []*GameInfo) error {
	seen := map[string]bool{}
	settings := managerSettings{}
	for _, game := range games {
		if game == nil || game.GameID != graveyardKeeperGameID || strings.TrimSpace(game.GameDirectory) == "" {
			continue
		}
		clean := filepath.Clean(game.GameDirectory)
		key := strings.ToLower(clean)
		if !seen[key] {
			seen[key] = true
			settings.GameDirectories = append(settings.GameDirectories, clean)
		}
	}
	sort.Slice(settings.GameDirectories, func(i, j int) bool {
		return strings.ToLower(settings.GameDirectories[i]) < strings.ToLower(settings.GameDirectories[j])
	})
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(managerSettingsPath(), append(data, '\n'), 0644)
}
