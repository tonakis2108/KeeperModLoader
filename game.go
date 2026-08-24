package main

import (
	"bufio"
	"debug/pe"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/sys/windows/registry"
)

type GameInfo struct {
	GameDirectory    string `json:"-"`
	ExecutablePath   string `json:"-"`
	ExecutableName   string `json:"executable"`
	ProcessName      string `json:"-"`
	GameID           string `json:"gameId"`
	SteamAppID       string `json:"steamAppId,omitempty"`
	DataDirectory    string `json:"dataDirectory"`
	ManagedDirectory string `json:"managedDirectory"`
	Backend          string `json:"backend"`
	Architecture     string `json:"architecture"`
	Supported        bool   `json:"-"`
	Reason           string `json:"-"`
}

func (g *GameInfo) String() string {
	status := "Available"
	if loaderEnabled(g) {
		installed := installedLoaderVersion(g)
		switch {
		case installed == "":
			status = "Enabled (version unknown)"
		case installed == loaderVersion:
			status = "Enabled " + installed
		default:
			status = "Update available " + installed + " → " + loaderVersion
		}
	}
	steam := ""
	if g.SteamAppID != "" {
		steam = "  |  Steam " + g.SteamAppID
	}
	return fmt.Sprintf("%s  |  %s %s  |  %s%s  |  %s", g.ProcessName, g.Backend, g.Architecture, status, steam, g.GameDirectory)
}

func normalizeGameID(name string) string {
	var b strings.Builder
	separator := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if separator && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			separator = false
		} else {
			separator = true
		}
	}
	if b.Len() == 0 {
		return "unknown-game"
	}
	return b.String()
}

func executableArchitecture(path string) string {
	f, err := pe.Open(path)
	if err != nil {
		return "Unknown"
	}
	defer f.Close()
	switch f.FileHeader.Machine {
	case pe.IMAGE_FILE_MACHINE_I386:
		return "x86"
	case pe.IMAGE_FILE_MACHINE_AMD64:
		return "x64"
	case 0xaa64:
		return "arm64"
	default:
		return "Unknown"
	}
}

func detectUnityGame(path string) (*GameInfo, error) {
	path = filepath.Clean(path)
	if absolute, absoluteErr := filepath.Abs(path); absoluteErr == nil {
		path = absolute
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	var executables []string
	if !info.IsDir() {
		if strings.EqualFold(filepath.Ext(path), ".exe") {
			executables = []string{path}
		}
	} else {
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return nil, readErr
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".exe") {
				continue
			}
			lower := strings.ToLower(entry.Name())
			if strings.HasPrefix(lower, "unitycrashhandler") || strings.HasPrefix(lower, "crashreportclient") {
				continue
			}
			executables = append(executables, filepath.Join(path, entry.Name()))
		}
		sort.Strings(executables)
	}

	for _, exe := range executables {
		base := strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
		gameDir := filepath.Dir(exe)
		dataDir := filepath.Join(gameDir, base+"_Data")
		if stat, statErr := os.Stat(dataDir); statErr != nil || !stat.IsDir() {
			continue
		}
		managedDir := filepath.Join(dataDir, "Managed")
		unityAssemblies, _ := filepath.Glob(filepath.Join(managedDir, "UnityEngine*.dll"))
		hasIL2CPP := fileExists(filepath.Join(gameDir, "GameAssembly.dll")) || dirExists(filepath.Join(dataDir, "il2cpp_data"))
		backend := "Unknown"
		if hasIL2CPP {
			backend = "IL2CPP"
		} else if len(unityAssemblies) > 0 && dirExists(managedDir) {
			backend = "Mono"
		}
		architecture := executableArchitecture(exe)
		supported := backend == "Mono" && (architecture == "x86" || architecture == "x64")
		reason := "Supported Unity Mono game."
		switch {
		case backend == "IL2CPP":
			reason = "IL2CPP detected. This KeeperLoader runtime supports Unity Mono games."
		case backend != "Mono":
			reason = "A supported Unity Mono Managed folder was not detected."
		case architecture != "x86" && architecture != "x64":
			reason = "Only Windows x86 and x64 Unity players are supported."
		}
		absoluteGameDir, _ := filepath.Abs(gameDir)
		absoluteExe, _ := filepath.Abs(exe)
		return &GameInfo{
			GameDirectory: absoluteGameDir, ExecutablePath: absoluteExe,
			ExecutableName: filepath.Base(exe), ProcessName: base, GameID: normalizeGameID(base),
			DataDirectory: dataDir, ManagedDirectory: managedDir, Backend: backend,
			Architecture: architecture, Supported: supported, Reason: reason,
		}, nil
	}
	return nil, errors.New("no Windows Unity player was detected in that location")
}

var steamPathPattern = regexp.MustCompile(`(?i)^\s*"path"\s*"([^"]+)"`)
var steamManifestValuePattern = regexp.MustCompile(`^\s*"([^"]+)"\s*"([^"]*)"`)
var steamAppIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,11}$`)

type steamAppManifest struct {
	AppID            string
	InstallDirectory string
}

func readSteamAppManifest(path string) (steamAppManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return steamAppManifest{}, err
	}
	defer file.Close()
	manifest := steamAppManifest{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		match := steamManifestValuePattern.FindStringSubmatch(scanner.Text())
		if len(match) != 3 {
			continue
		}
		switch strings.ToLower(match[1]) {
		case "appid":
			manifest.AppID = strings.TrimSpace(match[2])
		case "installdir":
			manifest.InstallDirectory = strings.TrimSpace(match[2])
		}
	}
	if err = scanner.Err(); err != nil {
		return steamAppManifest{}, err
	}
	if !steamAppIDPattern.MatchString(manifest.AppID) || manifest.InstallDirectory == "" {
		return steamAppManifest{}, errors.New("Steam app manifest is missing a valid app ID or install directory")
	}
	return manifest, nil
}

func steamInstallations(root string) map[string]string {
	result := map[string]string{}
	steamApps := filepath.Join(root, "steamapps")
	common := filepath.Join(steamApps, "common")
	manifestPaths, _ := filepath.Glob(filepath.Join(steamApps, "appmanifest_*.acf"))
	for _, manifestPath := range manifestPaths {
		manifest, err := readSteamAppManifest(manifestPath)
		if err != nil {
			continue
		}
		installPath := filepath.Clean(filepath.Join(common, filepath.FromSlash(manifest.InstallDirectory)))
		relative, relErr := filepath.Rel(common, installPath)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			continue
		}
		result[strings.ToLower(installPath)] = manifest.AppID
	}
	return result
}

func steamAppIDForGameDirectoryFromRoots(gameDirectory string, roots []string) string {
	target := strings.ToLower(filepath.Clean(gameDirectory))
	for _, root := range roots {
		if appID := steamInstallations(root)[target]; appID != "" {
			return appID
		}
	}
	return ""
}

func steamAppIDForGameDirectory(gameDirectory string) string {
	return steamAppIDForGameDirectoryFromRoots(gameDirectory, steamRoots())
}

func steamRunURI(appID string) (string, error) {
	appID = strings.TrimSpace(appID)
	if !steamAppIDPattern.MatchString(appID) {
		return "", errors.New("a valid Steam App ID was not found for this game")
	}
	return "steam://run/" + appID, nil
}

func steamRoots() []string {
	var roots []string
	if value := os.Getenv("ProgramFiles(x86)"); value != "" {
		roots = append(roots, filepath.Join(value, "Steam"))
	}
	if value := os.Getenv("ProgramFiles"); value != "" {
		roots = append(roots, filepath.Join(value, "Steam"))
	}
	if key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE); err == nil {
		if value, _, valueErr := key.GetStringValue("SteamPath"); valueErr == nil {
			roots = append(roots, filepath.FromSlash(value))
		}
		key.Close()
	}

	seen := map[string]bool{}
	var result []string
	add := func(value string) {
		value = filepath.Clean(value)
		key := strings.ToLower(value)
		if value != "." && dirExists(value) && !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	for _, root := range roots {
		add(root)
		vdf, err := os.Open(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(vdf)
		for scanner.Scan() {
			match := steamPathPattern.FindStringSubmatch(scanner.Text())
			if len(match) == 2 {
				add(strings.ReplaceAll(match[1], `\\`, `\`))
			}
		}
		vdf.Close()
	}
	return result
}

func scanSteamGames() ([]*GameInfo, error) {
	seen := map[string]bool{}
	var games []*GameInfo
	for _, root := range steamRoots() {
		installations := steamInstallations(root)
		common := filepath.Join(root, "steamapps", "common")
		entries, err := os.ReadDir(common)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			game, detectErr := detectUnityGame(filepath.Join(common, entry.Name()))
			if detectErr != nil || !game.Supported {
				continue
			}
			key := strings.ToLower(game.GameDirectory)
			if !seen[key] {
				seen[key] = true
				game.SteamAppID = installations[key]
				games = append(games, game)
			}
		}
	}
	sort.Slice(games, func(i, j int) bool {
		return strings.ToLower(games[i].ProcessName) < strings.ToLower(games[j].ProcessName)
	})
	return games, nil
}
