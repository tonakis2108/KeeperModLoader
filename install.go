package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	runtimePayloadDirectory = "runtime"
	safeModeMarker          = "safe-mode.next"
)

var requiredRuntimePayloadFiles = []string{
	"KeeperLoader/core/KeeperLoader.API.dll",
	"KeeperLoader/core/KeeperLoader.Bootstrap.dll",
	"KeeperLoader/core/KeeperLoader.Runtime.dll",
	"KeeperLoader/state/game.json",
	"doorstop_config.ini",
	"winhttp.dll",
}

func loaderBootstrapActive(game *GameInfo) bool {
	config := filepath.Join(game.GameDirectory, "doorstop_config.ini")
	if !fileExists(config) {
		return false
	}
	data, err := os.ReadFile(config)
	if err != nil {
		return false
	}
	normalized := strings.ToLower(strings.ReplaceAll(string(data), "/", `\`))
	for _, line := range strings.Split(normalized, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "target_assembly" && strings.TrimSpace(parts[1]) == `keeperloader\core\keeperloader.bootstrap.dll` {
			return true
		}
	}
	return false
}

func loaderEnabled(game *GameInfo) bool {
	return loaderBootstrapActive(game) &&
		fileExists(filepath.Join(game.GameDirectory, "winhttp.dll")) &&
		fileExists(filepath.Join(game.GameDirectory, "KeeperLoader", "core", "KeeperLoader.API.dll"))
}

func installedLoaderVersion(game *GameInfo) string {
	data, err := os.ReadFile(filepath.Join(game.GameDirectory, "KeeperLoader", "state", "game.json"))
	if err != nil {
		return ""
	}
	var record struct {
		KeeperLoaderVersion string `json:"keeperLoaderVersion"`
	}
	if json.Unmarshal(data, &record) != nil {
		return ""
	}
	return strings.TrimSpace(record.KeeperLoaderVersion)
}

func activateCoreUpdate(loader string, builtFiles [][2]string) (string, func(), error) {
	staging, err := os.MkdirTemp(loader, ".core-update-")
	if err != nil {
		return "", nil, err
	}
	stagingActive := true
	defer func() {
		if stagingActive {
			_ = os.RemoveAll(staging)
		}
	}()
	for _, pair := range builtFiles {
		if err = copyFile(pair[0], filepath.Join(staging, filepath.Base(pair[1]))); err != nil {
			return "", nil, err
		}
	}
	core := filepath.Join(loader, "core")
	backup := ""
	if dirExists(core) {
		backupRoot := filepath.Join(loader, "backup", "core")
		if err = os.MkdirAll(backupRoot, 0755); err != nil {
			return "", nil, err
		}
		backup = uniqueTimestampPath(backupRoot, "core")
		if err = os.Rename(core, backup); err != nil {
			return "", nil, err
		}
	}
	if err = os.Rename(staging, core); err != nil {
		if backup != "" {
			_ = os.Rename(backup, core)
		}
		return "", nil, err
	}
	stagingActive = false
	rollback := func() {
		_ = os.RemoveAll(core)
		if backup != "" {
			_ = os.Rename(backup, core)
		}
	}
	return backup, rollback, nil
}

func runtimePayloadRoot() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(executable), runtimePayloadDirectory), nil
}

func validateRuntimePayload(root string) error {
	return validateRuntimePayloadForVersion(root, loaderVersion)
}

func validateRuntimePayloadForVersion(root, expectedVersion string) error {
	checksumPath := filepath.Join(root, "SHA256SUMS.txt")
	data, err := os.ReadFile(checksumPath)
	if err != nil {
		return errors.New("the manager runtime payload is missing; extract the complete KeeperLoader Manager ZIP before running it")
	}
	expected := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !sha256Pattern.MatchString(fields[0]) {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		name = strings.ReplaceAll(name, `\`, "/")
		clean, pathErr := normalizedArchivePath(name)
		if pathErr != nil {
			return fmt.Errorf("runtime payload checksum path rejected: %w", pathErr)
		}
		extension := strings.ToLower(filepath.Ext(clean))
		switch extension {
		case ".exe", ".msi", ".bat", ".cmd", ".ps1", ".vbs", ".js":
			return fmt.Errorf("runtime payload contains blocked file type %q", extension)
		}
		expected[clean] = strings.ToLower(fields[0])
	}
	for _, required := range requiredRuntimePayloadFiles {
		if _, ok := expected[required]; !ok {
			return fmt.Errorf("runtime payload checksum is missing %s", required)
		}
	}
	for name, digest := range expected {
		actual, hashErr := fileSHA256(filepath.Join(root, filepath.FromSlash(name)))
		if hashErr != nil || !strings.EqualFold(actual, digest) {
			return fmt.Errorf("runtime payload integrity check failed for %s", name)
		}
	}
	gameRecord, err := os.ReadFile(filepath.Join(root, "KeeperLoader", "state", "game.json"))
	if err != nil {
		return err
	}
	var record struct {
		GameID              string `json:"gameId"`
		Architecture        string `json:"architecture"`
		KeeperLoaderVersion string `json:"keeperLoaderVersion"`
	}
	if json.Unmarshal(gameRecord, &record) != nil || record.GameID != graveyardKeeperGameID || record.Architecture != "x64" || record.KeeperLoaderVersion != expectedVersion {
		return fmt.Errorf("runtime payload metadata does not match expected KeeperLoader version %s", expectedVersion)
	}
	return nil
}

func enableLoader(game *GameInfo) (string, error) {
	if !game.Supported {
		return "", errors.New(game.Reason)
	}
	if err := assertGameStopped(game); err != nil {
		return "", err
	}
	if game.Architecture != "x64" {
		return "", errors.New("KeeperLoader currently supports only the x64 Steam build of Graveyard Keeper")
	}
	payload, err := runtimePayloadRoot()
	if err != nil {
		return "", err
	}
	if err = validateRuntimePayload(payload); err != nil {
		return "", err
	}
	apiBuild := filepath.Join(payload, "KeeperLoader", "core", "KeeperLoader.API.dll")
	bootstrapBuild := filepath.Join(payload, "KeeperLoader", "core", "KeeperLoader.Bootstrap.dll")
	runtimeBuild := filepath.Join(payload, "KeeperLoader", "core", "KeeperLoader.Runtime.dll")
	proxy := filepath.Join(payload, "winhttp.dll")
	configSource := filepath.Join(payload, "doorstop_config.ini")

	loader := filepath.Join(game.GameDirectory, "KeeperLoader")
	for _, directory := range []string{loader, filepath.Join(loader, "mods"), filepath.Join(loader, "config"), filepath.Join(loader, "logs"), filepath.Join(loader, "state")} {
		if err = os.MkdirAll(directory, 0755); err != nil {
			return "", err
		}
	}
	core := filepath.Join(loader, "core")
	_, rollbackCore, err := activateCoreUpdate(loader, [][2]string{
		{apiBuild, filepath.Join(core, "KeeperLoader.API.dll")},
		{bootstrapBuild, filepath.Join(core, "KeeperLoader.Bootstrap.dll")},
		{runtimeBuild, filepath.Join(core, "KeeperLoader.Runtime.dll")},
	})
	if err != nil {
		return "", err
	}
	coreCommitted := false
	defer func() {
		if !coreCommitted {
			rollbackCore()
		}
	}()

	backupMarker := filepath.Join(loader, "state", "last-backup.txt")
	currentConfig := filepath.Join(game.GameDirectory, "doorstop_config.ini")
	backup := ""
	if loaderBootstrapActive(game) {
		if data, readErr := os.ReadFile(backupMarker); readErr == nil && dirExists(strings.TrimSpace(string(data))) {
			backup = strings.TrimSpace(string(data))
		}
	}
	if backup == "" {
		backup = filepath.Join(loader, "backup", timestamp())
		if err = os.MkdirAll(backup, 0755); err != nil {
			return "", err
		}
		for _, name := range []string{"winhttp.dll", "doorstop_config.ini"} {
			existing := filepath.Join(game.GameDirectory, name)
			if fileExists(existing) {
				if err = copyFile(existing, filepath.Join(backup, name)); err != nil {
					return "", err
				}
			}
		}
		if err = writeAtomic(backupMarker, []byte(backup+"\r\n"), 0644); err != nil {
			return "", err
		}
	}
	if err = copyFile(proxy, filepath.Join(game.GameDirectory, "winhttp.dll")); err != nil {
		return "", err
	}
	config, err := os.ReadFile(configSource)
	if err != nil {
		return "", err
	}
	if err = writeAtomic(currentConfig, config, 0644); err != nil {
		return "", err
	}
	record := struct {
		GameID              string `json:"gameId"`
		Executable          string `json:"executable"`
		Backend             string `json:"backend"`
		Architecture        string `json:"architecture"`
		DataDirectory       string `json:"dataDirectory"`
		ManagedDirectory    string `json:"managedDirectory"`
		KeeperLoaderVersion string `json:"keeperLoaderVersion"`
	}{game.GameID, game.ExecutableName, game.Backend, game.Architecture, game.DataDirectory, game.ManagedDirectory, loaderVersion}
	jsonData, _ := json.MarshalIndent(record, "", "  ")
	if err = writeAtomic(filepath.Join(loader, "state", "game.json"), append(jsonData, '\n'), 0644); err != nil {
		return "", err
	}
	coreCommitted = true
	return backup, nil
}

func disableLoader(game *GameInfo) (string, error) {
	if err := assertGameStopped(game); err != nil {
		return "", err
	}
	loader := filepath.Join(game.GameDirectory, "KeeperLoader")
	active := loaderBootstrapActive(game)
	disabledProxy := filepath.Join(game.GameDirectory, "winhttp.keeperloader-disabled.dll")
	if !active && !dirExists(loader) && !fileExists(disabledProxy) {
		return "KeeperLoader was already absent; no files were changed.", nil
	}
	marker := filepath.Join(loader, "state", "last-backup.txt")
	backup := ""
	if data, err := os.ReadFile(marker); err == nil {
		backup = strings.TrimSpace(string(data))
	}
	if active {
		for _, name := range []string{"winhttp.dll", "doorstop_config.ini"} {
			current := filepath.Join(game.GameDirectory, name)
			if err := os.Remove(current); err != nil && !os.IsNotExist(err) {
				return "", err
			}
			if backup != "" {
				original := filepath.Join(backup, name)
				if fileExists(original) {
					if err := copyFile(original, current); err != nil {
						return "", err
					}
				}
			}
		}
	}
	if err := os.Remove(disabledProxy); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.RemoveAll(loader); err != nil {
		return "", err
	}
	return "KeeperLoader removed completely. Original bootstrap files were restored when available; game saves were not touched.", nil
}

func safeModeNextLaunchRequested(game *GameInfo) bool {
	return fileExists(filepath.Join(game.GameDirectory, "KeeperLoader", "state", safeModeMarker))
}

func setSafeModeNextLaunch(game *GameInfo, requested bool) (string, error) {
	if err := assertGameStopped(game); err != nil {
		return "", err
	}
	loader := filepath.Join(game.GameDirectory, "KeeperLoader")
	if !dirExists(loader) {
		return "", errors.New("KeeperLoader is not installed for this game")
	}
	marker := filepath.Join(loader, "state", safeModeMarker)
	if !requested {
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return "Safe mode request cancelled. Mods will load normally on the next launch.", nil
	}
	record := "requested_by_user=true\r\nrequested_at_utc=" + time.Now().UTC().Format(time.RFC3339Nano) + "\r\n"
	if err := writeAtomic(marker, []byte(record), 0644); err != nil {
		return "", err
	}
	return "Safe mode selected for the next launch only. Mods will be skipped once.", nil
}
