package main

import (
	"archive/zip"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	doorstopURL    = "https://github.com/NeighTools/UnityDoorstop/releases/download/v4.5.0/doorstop_win_release_4.5.0.zip"
	doorstopSHA256 = "7bb953e8d883c8bde76ced96f6d0e45660ad6e0151880d8ab5856bf4f532b147"
	safeModeMarker = "safe-mode.next"
)

//go:embed src/API/KeeperLoaderApi.cs src/Bootstrap/Entrypoint.cs src/Runtime/*.cs
var runtimeSources embed.FS

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

func extractEmbeddedSources(destination string) (map[string]string, error) {
	paths := []string{
		"src/API/KeeperLoaderApi.cs",
		"src/Bootstrap/Entrypoint.cs",
		"src/Runtime/RuntimeHost.cs",
		"src/Runtime/FileLogger.cs",
		"src/Runtime/ModCatalog.cs",
		"src/Runtime/ExternalPluginCatalog.cs",
	}
	result := make(map[string]string, len(paths))
	for _, path := range paths {
		data, err := runtimeSources.ReadFile(path)
		if err != nil {
			return nil, err
		}
		output := filepath.Join(destination, filepath.Base(path))
		if err = os.WriteFile(output, data, 0644); err != nil {
			return nil, err
		}
		result[filepath.Base(path)] = output
	}
	return result, nil
}

func unityReferences(managedDirectory string) ([]string, error) {
	references, err := filepath.Glob(filepath.Join(managedDirectory, "UnityEngine*.dll"))
	if err != nil || len(references) == 0 {
		return nil, errors.New("no UnityEngine assemblies were found in the game's Managed folder")
	}
	sort.Strings(references)
	return references, nil
}

func cachedDoorstopArchive() (string, error) {
	cacheBase := os.Getenv("LOCALAPPDATA")
	if cacheBase == "" {
		cacheBase = os.TempDir()
	}
	cache := filepath.Join(cacheBase, "KeeperLoader", "cache")
	if err := os.MkdirAll(cache, 0755); err != nil {
		return "", err
	}
	archivePath := filepath.Join(cache, "doorstop_win_release_4.5.0.zip")
	if fileExists(archivePath) {
		if digest, err := fileSHA256(archivePath); err == nil && strings.EqualFold(digest, doorstopSHA256) {
			return archivePath, nil
		}
		_ = os.Remove(archivePath)
	}

	client := &http.Client{Timeout: 90 * time.Second}
	request, _ := http.NewRequest(http.MethodGet, doorstopURL, nil)
	request.Header.Set("User-Agent", "KeeperLoader-Manager/"+loaderVersion)
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("could not download the UnityDoorstop bootstrap: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("UnityDoorstop download returned HTTP %s", response.Status)
	}
	temporary := archivePath + ".download"
	out, err := os.Create(temporary)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(out, io.LimitReader(response.Body, 32*1024*1024))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(temporary)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return "", closeErr
	}
	digest, err := fileSHA256(temporary)
	if err != nil || !strings.EqualFold(digest, doorstopSHA256) {
		_ = os.Remove(temporary)
		return "", errors.New("UnityDoorstop download failed its pinned SHA-256 integrity check")
	}
	_ = os.Remove(archivePath)
	if err = os.Rename(temporary, archivePath); err != nil {
		return "", err
	}
	return archivePath, nil
}

func doorstopProxy(archivePath, architecture, temporary string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	architectureMarkers := []string{"x64", "win64"}
	if architecture == "x86" {
		architectureMarkers = []string{"x86", "win32"}
	}
	for _, file := range reader.File {
		lower := strings.ToLower(strings.ReplaceAll(file.Name, `\`, "/"))
		if filepath.Base(lower) != "winhttp.dll" {
			continue
		}
		matched := false
		for _, marker := range architectureMarkers {
			if strings.Contains(lower, marker) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		input, openErr := file.Open()
		if openErr != nil {
			return "", openErr
		}
		outputPath := filepath.Join(temporary, "winhttp.dll")
		output, createErr := os.Create(outputPath)
		if createErr != nil {
			input.Close()
			return "", createErr
		}
		_, copyErr := io.Copy(output, input)
		input.Close()
		closeErr := output.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return outputPath, nil
	}
	return "", fmt.Errorf("the official UnityDoorstop archive did not contain a %s winhttp.dll", architecture)
}

func enableLoader(game *GameInfo) (string, error) {
	if !game.Supported {
		return "", errors.New(game.Reason)
	}
	if err := assertGameStopped(game); err != nil {
		return "", err
	}
	compiler, err := findCSharpCompiler()
	if err != nil {
		return "", err
	}
	references, err := unityReferences(game.ManagedDirectory)
	if err != nil {
		return "", err
	}
	temporary, err := os.MkdirTemp("", "KeeperLoader-build-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temporary)
	sources, err := extractEmbeddedSources(temporary)
	if err != nil {
		return "", err
	}
	apiBuild := filepath.Join(temporary, "KeeperLoader.API.dll")
	bootstrapBuild := filepath.Join(temporary, "KeeperLoader.Bootstrap.dll")
	runtimeBuild := filepath.Join(temporary, "KeeperLoader.Runtime.dll")
	if err = runCompiler(compiler, apiBuild, []string{sources["KeeperLoaderApi.cs"]}, nil); err != nil {
		return "", err
	}
	if err = runCompiler(compiler, bootstrapBuild, []string{sources["Entrypoint.cs"]}, nil); err != nil {
		return "", err
	}
	runtimeSourceList := []string{sources["RuntimeHost.cs"], sources["FileLogger.cs"], sources["ModCatalog.cs"], sources["ExternalPluginCatalog.cs"]}
	if err = runCompiler(compiler, runtimeBuild, runtimeSourceList, append([]string{apiBuild}, references...)); err != nil {
		return "", err
	}

	archive, err := cachedDoorstopArchive()
	if err != nil {
		return "", err
	}
	proxy, err := doorstopProxy(archive, game.Architecture, temporary)
	if err != nil {
		return "", err
	}

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
	config := "[General]\r\nenabled=true\r\ntarget_assembly=KeeperLoader\\core\\KeeperLoader.Bootstrap.dll\r\nredirect_output_log=false\r\nboot_config_override=\r\nignore_disable_switch=false\r\n\r\n[UnityMono]\r\ndll_search_path_override=\r\ndebug_enabled=false\r\ndebug_address=127.0.0.1:10000\r\ndebug_suspend=false\r\n\r\n[Il2Cpp]\r\ncoreclr_path=\r\ncorlib_dir=\r\n"
	if err = writeAtomic(currentConfig, []byte(config), 0644); err != nil {
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
