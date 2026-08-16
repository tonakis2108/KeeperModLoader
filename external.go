package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	externalPluginMode       = "external-unity-plugin"
	externalPluginStatusName = "external-plugin.status"
)

var externalVersionPattern = regexp.MustCompile(`(?i)(?:^|[-_. ])v?(\d+\.\d+(?:\.\d+){0,2})(?:$|[-_. ])`)

type externalPackageMetadata struct {
	Name          string `json:"name"`
	VersionNumber string `json:"version_number"`
}

func readExternalPluginStatus(game *GameInfo, modID string) string {
	data, err := os.ReadFile(filepath.Join(game.GameDirectory, "KeeperLoader", "state", modID, externalPluginStatusName))
	if err != nil {
		return "pending"
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "status") {
			value := strings.ToLower(strings.TrimSpace(parts[1]))
			if value == "loaded" || value == "incompatible" || value == "pending" {
				return value
			}
		}
	}
	return "pending"
}

func writeExternalPluginStatus(game *GameInfo, modID, status, message string) error {
	directory := filepath.Join(game.GameDirectory, "KeeperLoader", "state", modID)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	message = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(message), "\r", " "), "\n", " ")
	record := "status=" + status + "\r\nmessage=" + message + "\r\nupdated_at_utc=" + time.Now().UTC().Format(time.RFC3339Nano) + "\r\n"
	return writeAtomic(filepath.Join(directory, externalPluginStatusName), []byte(record), 0644)
}

func externalPackageIdentity(zipPath string, archiveFiles map[string]string) (string, string, string) {
	name := strings.TrimSuffix(filepath.Base(zipPath), filepath.Ext(zipPath))
	version := "0.0.0"
	metadataPath := archiveFiles["manifest.json"]
	if metadataPath == "" {
		var candidates []string
		for key := range archiveFiles {
			if strings.EqualFold(filepath.Base(filepath.FromSlash(key)), "manifest.json") {
				candidates = append(candidates, key)
			}
		}
		sort.Strings(candidates)
		if len(candidates) == 1 {
			metadataPath = archiveFiles[candidates[0]]
		}
	}
	if metadataPath != "" {
		data, err := os.ReadFile(metadataPath)
		var metadata externalPackageMetadata
		if err == nil && json.Unmarshal(data, &metadata) == nil {
			if strings.TrimSpace(metadata.Name) != "" {
				name = strings.TrimSpace(metadata.Name)
			}
			if _, versionErr := parseVersion(strings.TrimSpace(metadata.VersionNumber)); versionErr == nil {
				version = strings.TrimSpace(metadata.VersionNumber)
			}
		}
	}
	if version == "0.0.0" {
		match := externalVersionPattern.FindStringSubmatch(name)
		if len(match) == 2 {
			if _, err := parseVersion(match[1]); err == nil {
				version = match[1]
			}
		}
	}
	id := externalPluginID(name)
	return id, name, version
}

func externalPluginID(name string) string {
	var result strings.Builder
	separator := false
	for _, char := range strings.ToLower(name) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			if separator && result.Len() > 0 {
				result.WriteByte('-')
			}
			result.WriteRune(char)
			separator = false
		} else {
			separator = true
		}
		if result.Len() >= 70 {
			break
		}
	}
	value := strings.Trim(result.String(), "-")
	if value == "" {
		value = "plugin"
	}
	return "external." + value
}

func installExternalPluginPackage(game *GameInfo, zipPath string) (*InstalledMod, string, error) {
	return installExternalPluginPackageWithID(game, zipPath, "")
}

func updateExternalPluginPackage(game *GameInfo, current *InstalledMod, zipPath string) (*InstalledMod, string, error) {
	if current == nil || current.Mode != externalPluginMode || !modIDPattern.MatchString(current.ID) {
		return nil, "", errors.New("select a valid external plugin first")
	}
	return installExternalPluginPackageWithID(game, zipPath, current.ID)
}

func installExternalPluginPackageWithID(game *GameInfo, zipPath, expectedID string) (*InstalledMod, string, error) {
	if err := assertGameStopped(game); err != nil {
		return nil, "", err
	}
	installedVersion, err := validateGameRecord(game)
	if err != nil {
		return nil, "", err
	}
	minimumVersion, _ := parseVersion("0.6.0")
	if compareVersion(installedVersion, minimumVersion) < 0 {
		return nil, "", errors.New("external plugin support requires the KeeperLoader 0.6.0 game core; select the game and use Enable / update selected first")
	}
	temporary, err := os.MkdirTemp("", "KeeperExternal-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(temporary)
	extractRoot := filepath.Join(temporary, "payload")
	if err = os.MkdirAll(extractRoot, 0755); err != nil {
		return nil, "", err
	}
	archiveFiles, err := extractVerifiedArchive(zipPath, extractRoot)
	if err != nil {
		return nil, "", err
	}
	if _, nativePackage := archiveFiles["keepermod.json"]; nativePackage {
		return nil, "", errors.New("external plugin package rejected: this is a native KeeperLoader package; use Install Mod ZIP")
	}
	if len(archiveFiles) == 0 {
		return nil, "", errors.New("external plugin package rejected: the ZIP is empty")
	}
	hasDLL := false
	for key := range archiveFiles {
		if reservedModControlPath(key) {
			return nil, "", fmt.Errorf("external plugin package rejected: reserved KeeperLoader control path %q", key)
		}
		extension := strings.ToLower(filepath.Ext(key))
		if blockedExtensions[extension] {
			return nil, "", fmt.Errorf("external plugin package rejected: blocked executable or script %q", key)
		}
		if extension == ".dll" {
			hasDLL = true
		}
	}
	if !hasDLL {
		return nil, "", errors.New("external plugin package rejected: no DLL was found")
	}
	id, name, version := externalPackageIdentity(zipPath, archiveFiles)
	if expectedID != "" {
		id = expectedID
	}
	if !modIDPattern.MatchString(id) {
		return nil, "", errors.New("external plugin package rejected: generated package id is invalid")
	}

	manifest := ModManifest{
		ID: id, Name: name, Version: version, EntryMode: externalPluginMode,
		MinimumKeeperLoaderVersion: loaderVersion, UnityBackend: "Mono",
		SupportedGames: []string{game.GameID},
	}
	keys := make([]string, 0, len(archiveFiles))
	for key := range archiveFiles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		digest, hashErr := fileSHA256(archiveFiles[key])
		if hashErr != nil {
			return nil, "", hashErr
		}
		manifest.Files = append(manifest.Files, ManifestFile{Path: key, SHA256: digest})
	}

	loader := filepath.Join(game.GameDirectory, "KeeperLoader")
	modsRoot := filepath.Join(loader, "mods")
	if err = os.MkdirAll(modsRoot, 0755); err != nil {
		return nil, "", err
	}
	staging, err := os.MkdirTemp(modsRoot, ".install-")
	if err != nil {
		return nil, "", err
	}
	stagingActive := true
	defer func() {
		if stagingActive {
			_ = os.RemoveAll(staging)
		}
	}()
	for _, key := range keys {
		if err = copyFile(archiveFiles[key], filepath.Join(staging, filepath.FromSlash(key))); err != nil {
			return nil, "", err
		}
	}
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	if err = os.WriteFile(filepath.Join(staging, "keepermod.json"), append(manifestData, '\n'), 0644); err != nil {
		return nil, "", err
	}
	activation := fmt.Sprintf("game_id=%s\r\nbackend=Mono\r\ncompatibility=explicit-game-id\r\nentry_mode=%s\r\nmod_id=%s\r\npackage_version=%s\r\nkeeperloader_version=%s\r\n", game.GameID, externalPluginMode, id, version, loaderVersion)
	if err = os.WriteFile(filepath.Join(staging, "keeperloader.activation"), []byte(activation), 0644); err != nil {
		return nil, "", err
	}

	target := filepath.Join(modsRoot, id)
	keepDisabled := dirExists(target) && fileExists(filepath.Join(target, modDisabledMarker))
	if keepDisabled {
		if err = os.WriteFile(filepath.Join(staging, modDisabledMarker), []byte("disabled_by_manager=true\r\n"), 0644); err != nil {
			return nil, "", err
		}
	}
	backup := ""
	if dirExists(target) {
		backupRoot := filepath.Join(loader, "backup", "mods")
		if err = os.MkdirAll(backupRoot, 0755); err != nil {
			return nil, "", err
		}
		backup = uniqueTimestampPath(backupRoot, id)
		if err = os.Rename(target, backup); err != nil {
			return nil, "", err
		}
		now := time.Now()
		_ = os.Chtimes(backup, now, now)
	}
	if err = os.Rename(staging, target); err != nil {
		if backup != "" && !dirExists(target) {
			_ = os.Rename(backup, target)
		}
		return nil, "", err
	}
	stagingActive = false
	_ = writeExternalPluginStatus(game, id, "pending", "The package will be tested when the game next starts.")
	return &InstalledMod{ID: id, Name: name, Version: version, Path: target, Enabled: !keepDisabled, Mode: externalPluginMode, Status: "pending"}, backup, nil
}
