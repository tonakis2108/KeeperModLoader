package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type ManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type BuildSpec struct {
	Sources []string `json:"sources"`
	Output  string   `json:"output"`
}

type ModManifest struct {
	ID                         string         `json:"id"`
	Name                       string         `json:"name"`
	Version                    string         `json:"version"`
	EntryMode                  string         `json:"entryMode,omitempty"`
	MinimumKeeperLoaderVersion string         `json:"minimumKeeperLoaderVersion"`
	UnityBackend               string         `json:"unityBackend"`
	SupportedGames             []string       `json:"supportedGames"`
	Files                      []ManifestFile `json:"files"`
	Build                      *BuildSpec     `json:"build,omitempty"`
}

type InstalledMod struct {
	ID      string
	Name    string
	Version string
	Path    string
	Enabled bool
	Mode    string
	Status  string
}

func (m *InstalledMod) String() string {
	status := "Disabled"
	if m.Enabled {
		status = "Enabled"
	}
	kind := ""
	if m.Mode == legacyExternalPluginMode {
		kind = "  [Legacy external: Inactive]"
	}
	return fmt.Sprintf("[%s]%s  %s  |  %s  |  %s", status, kind, m.Name, m.Version, m.ID)
}

const (
	modDisabledMarker        = "keeperloader.disabled"
	legacyExternalPluginMode = "external-unity-plugin"
)

func reservedModControlPath(name string) bool {
	key := strings.ToLower(filepath.ToSlash(name))
	return key == "keepermod.json" || key == "keeperloader.activation" || key == modDisabledMarker
}

var (
	modIDPattern       = regexp.MustCompile(`^[A-Za-z0-9._-]{1,80}$`)
	gameIDPattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	buildOutputPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+\.dll$`)
	sha256Pattern      = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
	blockedExtensions  = map[string]bool{
		".exe": true, ".com": true, ".bat": true, ".cmd": true, ".ps1": true,
		".vbs": true, ".js": true, ".jse": true, ".wsf": true, ".wsh": true,
		".msi": true, ".msp": true, ".scr": true, ".lnk": true,
	}
)

func installedMods(game *GameInfo) ([]*InstalledMod, error) {
	modsRoot := filepath.Join(game.GameDirectory, "KeeperLoader", "mods")
	entries, err := os.ReadDir(modsRoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []*InstalledMod
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		mod := &InstalledMod{ID: entry.Name(), Name: entry.Name(), Version: "unknown", Path: filepath.Join(modsRoot, entry.Name()), Enabled: true}
		mod.Enabled = !fileExists(filepath.Join(mod.Path, modDisabledMarker))
		if data, readErr := os.ReadFile(filepath.Join(mod.Path, "keepermod.json")); readErr == nil {
			var manifest ModManifest
			if json.Unmarshal(data, &manifest) == nil {
				if strings.TrimSpace(manifest.Name) != "" {
					mod.Name = manifest.Name
				}
				if strings.TrimSpace(manifest.Version) != "" {
					mod.Version = manifest.Version
				}
				mod.Mode = manifest.EntryMode
			}
		}
		if strings.TrimSpace(mod.Mode) == "" {
			mod.Mode = "native"
		}
		if mod.Mode == legacyExternalPluginMode {
			// External plugin loading was removed in 0.6.2. Keep old packages visible
			// so users can uninstall them, but never imply that they can still load.
			mod.Enabled = false
			mod.Status = "inactive"
		}
		result = append(result, mod)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result, nil
}

func normalizedArchivePath(name string) (string, error) {
	name = strings.ReplaceAll(name, `\`, "/")
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimRight(name, "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == ".." || part == "." || part == "" {
			return "", fmt.Errorf("unsafe archive path %q", name)
		}
	}
	return strings.Join(parts, "/"), nil
}

func extractVerifiedArchive(zipPath, destination string) (map[string]string, error) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("mod package rejected: select a valid ZIP file: %w", err)
	}
	defer archive.Close()
	if len(archive.File) > 2048 {
		return nil, errors.New("mod package rejected: the ZIP contains more than 2048 entries")
	}
	seen := map[string]bool{}
	files := map[string]string{}
	var expanded uint64
	for _, entry := range archive.File {
		name, pathErr := normalizedArchivePath(entry.Name)
		if pathErr != nil {
			return nil, fmt.Errorf("mod package rejected: %w", pathErr)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("mod package rejected: duplicate archive path %q", name)
		}
		seen[key] = true
		expanded += entry.UncompressedSize64
		if expanded > 256*1024*1024 {
			return nil, errors.New("mod package rejected: expanded content exceeds 256 MB")
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		output := filepath.Join(destination, filepath.FromSlash(name))
		absoluteDestination, _ := filepath.Abs(destination)
		absoluteOutput, _ := filepath.Abs(output)
		if !strings.HasPrefix(strings.ToLower(absoluteOutput), strings.ToLower(absoluteDestination)+strings.ToLower(string(os.PathSeparator))) {
			return nil, fmt.Errorf("mod package rejected: unsafe archive path %q", name)
		}
		if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
			return nil, err
		}
		input, openErr := entry.Open()
		if openErr != nil {
			return nil, openErr
		}
		out, createErr := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if createErr != nil {
			input.Close()
			return nil, createErr
		}
		_, copyErr := io.Copy(out, io.LimitReader(input, int64(entry.UncompressedSize64)+1))
		input.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		files[key] = output
	}
	return files, nil
}

func validateGameRecord(game *GameInfo) ([4]int, error) {
	var zero [4]int
	data, err := os.ReadFile(filepath.Join(game.GameDirectory, "KeeperLoader", "state", "game.json"))
	if err != nil {
		return zero, errors.New("KeeperLoader must be enabled for this game before installing mods")
	}
	var record struct{ GameID, KeeperLoaderVersion string }
	if json.Unmarshal(data, &record) != nil || !strings.EqualFold(record.GameID, game.GameID) {
		return zero, errors.New("the installed KeeperLoader game record is invalid; enable KeeperLoader again")
	}
	version, err := parseVersion(record.KeeperLoaderVersion)
	if err != nil {
		return zero, errors.New("the installed KeeperLoader version is unknown; enable KeeperLoader again")
	}
	return version, nil
}

type modInstallOptions struct {
	ExpectedID     string
	CurrentVersion string
	RequireNewer   bool
}

func installModPackage(game *GameInfo, zipPath string) (*InstalledMod, string, error) {
	return installModPackageWithOptions(game, zipPath, modInstallOptions{})
}

func updateModPackage(game *GameInfo, current *InstalledMod, zipPath string) (*InstalledMod, string, error) {
	if current == nil || !modIDPattern.MatchString(current.ID) {
		return nil, "", errors.New("select a valid installed mod first")
	}
	if strings.EqualFold(current.Mode, legacyExternalPluginMode) {
		return nil, "", errors.New("external plugin support was removed; uninstall this inactive legacy package")
	}
	return installModPackageWithOptions(game, zipPath, modInstallOptions{
		ExpectedID: current.ID, CurrentVersion: current.Version, RequireNewer: true,
	})
}

func installModPackageWithOptions(game *GameInfo, zipPath string, options modInstallOptions) (*InstalledMod, string, error) {
	if err := assertGameStopped(game); err != nil {
		return nil, "", err
	}
	installedLoaderVersion, err := validateGameRecord(game)
	if err != nil {
		return nil, "", err
	}
	temporary, err := os.MkdirTemp("", "KeeperMod-")
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
	manifestPath, ok := archiveFiles["keepermod.json"]
	if !ok {
		return nil, "", errors.New("mod package rejected: keepermod.json is missing from the ZIP root")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, "", err
	}
	var manifest ModManifest
	if json.Unmarshal(data, &manifest) != nil {
		return nil, "", errors.New("mod package rejected: keepermod.json is not valid JSON")
	}
	if !modIDPattern.MatchString(manifest.ID) {
		return nil, "", errors.New("mod package rejected: manifest id is invalid")
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return nil, "", errors.New("mod package rejected: manifest name is required")
	}
	if _, err = parseVersion(manifest.Version); err != nil {
		return nil, "", errors.New("mod package rejected: manifest version is invalid")
	}
	if manifest.EntryMode != "" && !strings.EqualFold(manifest.EntryMode, "native") {
		if strings.EqualFold(manifest.EntryMode, legacyExternalPluginMode) {
			return nil, "", errors.New("mod package rejected: experimental external plugins are no longer supported")
		}
		return nil, "", errors.New("mod package rejected: entryMode must be omitted or native")
	}
	manifest.EntryMode = "native"
	if options.ExpectedID != "" && !strings.EqualFold(manifest.ID, options.ExpectedID) {
		return nil, "", fmt.Errorf("mod update rejected: selected mod is %s but the package contains %s", options.ExpectedID, manifest.ID)
	}
	if options.RequireNewer {
		currentVersion, currentErr := parseVersion(options.CurrentVersion)
		newVersion, newErr := parseVersion(manifest.Version)
		if currentErr != nil {
			return nil, "", errors.New("mod update rejected: the installed version is unknown; use Install Mod ZIP for a deliberate replacement")
		}
		if newErr != nil || compareVersion(newVersion, currentVersion) <= 0 {
			return nil, "", fmt.Errorf("mod update rejected: version %s must be newer than installed version %s", manifest.Version, options.CurrentVersion)
		}
	}
	minimum, err := parseVersion(manifest.MinimumKeeperLoaderVersion)
	if err != nil {
		return nil, "", errors.New("mod package rejected: minimumKeeperLoaderVersion is missing or invalid")
	}
	if compareVersion(minimum, installedLoaderVersion) > 0 {
		return nil, "", fmt.Errorf("mod package rejected: %s requires KeeperLoader %s or newer", manifest.Name, manifest.MinimumKeeperLoaderVersion)
	}
	if !strings.EqualFold(manifest.UnityBackend, "Mono") {
		return nil, "", errors.New("mod package rejected: unityBackend must be Mono")
	}
	if len(manifest.SupportedGames) == 0 {
		return nil, "", errors.New("mod package rejected: supportedGames is missing or empty")
	}
	compatible := false
	for _, id := range manifest.SupportedGames {
		id = strings.ToLower(id)
		if id == "*" {
			return nil, "", errors.New("mod package rejected: wildcard game compatibility is not permitted; list explicit game IDs")
		}
		if !gameIDPattern.MatchString(id) {
			return nil, "", fmt.Errorf("mod package rejected: invalid supported game id %q", id)
		}
		if id == strings.ToLower(game.GameID) {
			compatible = true
		}
	}
	if !compatible {
		return nil, "", fmt.Errorf("mod package rejected: %s does not declare compatibility with %s", manifest.Name, game.GameID)
	}
	if len(manifest.Files) == 0 {
		return nil, "", errors.New("mod package rejected: manifest files list is empty")
	}

	declared := map[string]ManifestFile{}
	hasDLL := false
	for _, file := range manifest.Files {
		name, pathErr := normalizedArchivePath(file.Path)
		if pathErr != nil {
			return nil, "", fmt.Errorf("mod package rejected: %w", pathErr)
		}
		key := strings.ToLower(name)
		if reservedModControlPath(key) {
			return nil, "", fmt.Errorf("mod package rejected: reserved KeeperLoader control path %q", name)
		}
		if _, duplicate := declared[key]; duplicate {
			return nil, "", fmt.Errorf("mod package rejected: duplicate manifest path %q", name)
		}
		if !sha256Pattern.MatchString(file.SHA256) {
			return nil, "", fmt.Errorf("mod package rejected: invalid SHA-256 for %q", name)
		}
		if blockedExtensions[strings.ToLower(filepath.Ext(name))] {
			return nil, "", fmt.Errorf("mod package rejected: blocked executable or script %q", name)
		}
		fullPath, present := archiveFiles[key]
		if !present {
			return nil, "", fmt.Errorf("mod package rejected: declared file %q is missing", name)
		}
		digest, hashErr := fileSHA256(fullPath)
		if hashErr != nil || !strings.EqualFold(digest, file.SHA256) {
			return nil, "", fmt.Errorf("mod package rejected: SHA-256 mismatch for %q", name)
		}
		file.Path = name
		declared[key] = file
		if strings.EqualFold(filepath.Ext(name), ".dll") {
			hasDLL = true
		}
	}
	if !hasDLL && manifest.Build == nil {
		return nil, "", errors.New("mod package rejected: package contains neither a DLL nor source build specification")
	}
	if len(archiveFiles)-1 != len(declared) {
		return nil, "", errors.New("mod package rejected: ZIP contains undeclared payload files")
	}
	for key := range archiveFiles {
		if key == "keepermod.json" {
			continue
		}
		if _, present := declared[key]; !present {
			return nil, "", fmt.Errorf("mod package rejected: undeclared file %q is present", key)
		}
	}

	if manifest.Build != nil {
		if len(manifest.Build.Sources) == 0 || !buildOutputPattern.MatchString(manifest.Build.Output) {
			return nil, "", errors.New("mod package rejected: source build specification is invalid")
		}
		for _, source := range manifest.Build.Sources {
			name, pathErr := normalizedArchivePath(source)
			if pathErr != nil || !strings.EqualFold(filepath.Ext(name), ".cs") {
				return nil, "", fmt.Errorf("mod package rejected: invalid build source %q", source)
			}
			if _, present := declared[strings.ToLower(name)]; !present {
				return nil, "", fmt.Errorf("mod package rejected: build source %q is not in the verified file list", source)
			}
		}
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
	for key, file := range declared {
		source := archiveFiles[key]
		destination := filepath.Join(staging, filepath.FromSlash(file.Path))
		if err = copyFile(source, destination); err != nil {
			return nil, "", err
		}
	}
	if err = copyFile(manifestPath, filepath.Join(staging, "keepermod.json")); err != nil {
		return nil, "", err
	}

	if manifest.Build != nil {
		compiler, compilerErr := findCSharpCompiler()
		if compilerErr != nil {
			return nil, "", compilerErr
		}
		references, refErr := unityReferences(game.ManagedDirectory)
		if refErr != nil {
			return nil, "", refErr
		}
		references = append([]string{filepath.Join(loader, "core", "KeeperLoader.API.dll")}, references...)
		var sources []string
		for _, source := range manifest.Build.Sources {
			sources = append(sources, filepath.Join(staging, filepath.FromSlash(source)))
		}
		if err = runCompiler(compiler, filepath.Join(staging, manifest.Build.Output), sources, references); err != nil {
			return nil, "", err
		}
	}
	activation := fmt.Sprintf("game_id=%s\r\nbackend=Mono\r\ncompatibility=explicit-game-id\r\nentry_mode=native\r\nmod_id=%s\r\npackage_version=%s\r\nkeeperloader_version=%s\r\n", game.GameID, manifest.ID, manifest.Version, loaderVersion)
	if err = os.WriteFile(filepath.Join(staging, "keeperloader.activation"), []byte(activation), 0644); err != nil {
		return nil, "", err
	}

	target := filepath.Join(modsRoot, manifest.ID)
	keepDisabled := dirExists(target) && fileExists(filepath.Join(target, modDisabledMarker))
	if keepDisabled {
		disabledRecord := "disabled_by_manager=true\r\n"
		if err = os.WriteFile(filepath.Join(staging, modDisabledMarker), []byte(disabledRecord), 0644); err != nil {
			return nil, "", err
		}
	}
	backup := ""
	if dirExists(target) {
		backupRoot := filepath.Join(loader, "backup", "mods")
		if err = os.MkdirAll(backupRoot, 0755); err != nil {
			return nil, "", err
		}
		backup = uniqueTimestampPath(backupRoot, manifest.ID)
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
	return &InstalledMod{ID: manifest.ID, Name: manifest.Name, Version: manifest.Version, Path: target, Enabled: !keepDisabled, Mode: manifest.EntryMode}, backup, nil
}

func validateInstalledModPath(game *GameInfo, mod *InstalledMod) error {
	if mod == nil || !modIDPattern.MatchString(mod.ID) || !dirExists(mod.Path) {
		return errors.New("the selected mod is not installed")
	}
	expected := filepath.Join(game.GameDirectory, "KeeperLoader", "mods", mod.ID)
	if !strings.EqualFold(filepath.Clean(mod.Path), filepath.Clean(expected)) {
		return errors.New("the selected mod path is outside the KeeperLoader mods directory")
	}
	return nil
}

func setModEnabled(game *GameInfo, mod *InstalledMod, enabled bool) (string, error) {
	if err := assertGameStopped(game); err != nil {
		return "", err
	}
	if err := validateInstalledModPath(game, mod); err != nil {
		return "", err
	}
	if enabled && strings.EqualFold(mod.Mode, legacyExternalPluginMode) {
		return "", errors.New("external plugin support was removed; this legacy package remains inactive and can only be uninstalled")
	}
	marker := filepath.Join(mod.Path, modDisabledMarker)
	if enabled {
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		mod.Enabled = true
		return mod.Name + " enabled. It will load the next time the game starts.", nil
	}
	record := "disabled_by_manager=true\r\ndisabled_at_utc=" + time.Now().UTC().Format(time.RFC3339Nano) + "\r\n"
	if err := writeAtomic(marker, []byte(record), 0644); err != nil {
		return "", err
	}
	mod.Enabled = false
	return mod.Name + " disabled. Files, configuration, state, backups, and saves were preserved.", nil
}

func uninstallMod(game *GameInfo, mod *InstalledMod) (string, error) {
	if err := assertGameStopped(game); err != nil {
		return "", err
	}
	if err := validateInstalledModPath(game, mod); err != nil {
		return "", err
	}
	loader := filepath.Join(game.GameDirectory, "KeeperLoader")
	for _, path := range []string{
		mod.Path,
		filepath.Join(loader, "config", mod.ID+".cfg"),
		filepath.Join(loader, "state", mod.ID),
	} {
		if err := os.RemoveAll(path); err != nil {
			return "", err
		}
	}
	for _, backupRoot := range []string{
		filepath.Join(loader, "backup", "mods"),
		filepath.Join(loader, "backup", "uninstalled"),
	} {
		entries, readErr := os.ReadDir(backupRoot)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			return "", readErr
		}
		prefix := strings.ToLower(mod.ID) + "-"
		for _, entry := range entries {
			if strings.HasPrefix(strings.ToLower(entry.Name()), prefix) {
				if err := os.RemoveAll(filepath.Join(backupRoot, entry.Name())); err != nil {
					return "", err
				}
			}
		}
	}
	return "Mod files, configuration, state, and backups were deleted. Game saves were not touched.", nil
}

func restorePreviousMod(game *GameInfo, current *InstalledMod) (*InstalledMod, string, error) {
	if err := assertGameStopped(game); err != nil {
		return nil, "", err
	}
	if err := validateInstalledModPath(game, current); err != nil {
		return nil, "", err
	}
	if strings.EqualFold(current.Mode, legacyExternalPluginMode) {
		return nil, "", errors.New("external plugin support was removed; uninstall this inactive legacy package")
	}
	keepDisabled := fileExists(filepath.Join(current.Path, modDisabledMarker))
	loader := filepath.Join(game.GameDirectory, "KeeperLoader")
	backupRoot := filepath.Join(loader, "backup", "mods")
	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", errors.New("no previous version is available for this mod")
		}
		return nil, "", err
	}
	type candidate struct {
		path     string
		modified time.Time
		manifest ModManifest
	}
	var candidates []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(backupRoot, entry.Name())
		data, readErr := os.ReadFile(filepath.Join(path, "keepermod.json"))
		if readErr != nil {
			continue
		}
		var manifest ModManifest
		if json.Unmarshal(data, &manifest) != nil || !strings.EqualFold(manifest.ID, current.ID) ||
			strings.EqualFold(manifest.EntryMode, legacyExternalPluginMode) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		candidates = append(candidates, candidate{path: path, modified: info.ModTime(), manifest: manifest})
	}
	if len(candidates) == 0 {
		return nil, "", errors.New("no previous version is available for this mod")
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].modified.After(candidates[j].modified) })
	previous := candidates[0]
	previousMarker := filepath.Join(previous.path, modDisabledMarker)
	if keepDisabled {
		if err = writeAtomic(previousMarker, []byte("disabled_by_manager=true\r\n"), 0644); err != nil {
			return nil, "", err
		}
	} else if err = os.Remove(previousMarker); err != nil && !os.IsNotExist(err) {
		return nil, "", err
	}
	currentBackup := uniqueTimestampPath(backupRoot, current.ID+"-before-restore")
	if err = os.Rename(current.Path, currentBackup); err != nil {
		return nil, "", err
	}
	now := time.Now()
	_ = os.Chtimes(currentBackup, now, now)
	if err = os.Rename(previous.path, current.Path); err != nil {
		_ = os.Rename(currentBackup, current.Path)
		return nil, "", err
	}
	mode := previous.manifest.EntryMode
	if strings.TrimSpace(mode) == "" {
		mode = "native"
	}
	return &InstalledMod{ID: previous.manifest.ID, Name: previous.manifest.Name, Version: previous.manifest.Version, Path: current.Path, Enabled: !keepDisabled, Mode: mode}, currentBackup, nil
}

func createModPackage(sourceFolder, modID, modName, version string, supportedGames []string, minimumVersion, outputZip string) error {
	if !dirExists(sourceFolder) {
		return errors.New("source folder does not exist")
	}
	if !modIDPattern.MatchString(modID) {
		return errors.New("mod ID is invalid")
	}
	if strings.TrimSpace(modName) == "" {
		return errors.New("mod name is required")
	}
	if _, err := parseVersion(version); err != nil {
		return errors.New("version is invalid")
	}
	if _, err := parseVersion(minimumVersion); err != nil {
		return errors.New("minimum KeeperLoader version is invalid")
	}
	if len(supportedGames) == 0 {
		return errors.New("supported games is required")
	}
	if strings.TrimSpace(outputZip) == "" {
		return errors.New("select an output ZIP path")
	}
	for _, id := range supportedGames {
		if id == "*" {
			return errors.New("wildcard compatibility is not permitted; list explicit game IDs")
		}
		if !gameIDPattern.MatchString(id) {
			return fmt.Errorf("invalid game ID %q", id)
		}
	}
	var files []string
	hasDLL := false
	err := filepath.Walk(sourceFolder, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		relative, _ := filepath.Rel(sourceFolder, path)
		if reservedModControlPath(relative) {
			return nil
		}
		if blockedExtensions[strings.ToLower(filepath.Ext(relative))] {
			return fmt.Errorf("blocked executable or script %q", relative)
		}
		if strings.EqualFold(filepath.Ext(relative), ".dll") {
			hasDLL = true
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return err
	}
	if len(files) == 0 || !hasDLL {
		return errors.New("source folder must contain at least one DLL")
	}
	sort.Strings(files)
	manifest := ModManifest{ID: modID, Name: modName, Version: version, MinimumKeeperLoaderVersion: minimumVersion, UnityBackend: "Mono", SupportedGames: supportedGames}
	for _, file := range files {
		relative, _ := filepath.Rel(sourceFolder, file)
		digest, hashErr := fileSHA256(file)
		if hashErr != nil {
			return hashErr
		}
		manifest.Files = append(manifest.Files, ManifestFile{Path: filepath.ToSlash(relative), SHA256: digest})
	}
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	if err = os.MkdirAll(filepath.Dir(outputZip), 0755); err != nil {
		return err
	}
	temporary := outputZip + ".tmp"
	_ = os.Remove(temporary)
	out, err := os.Create(temporary)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(out)
	manifestWriter, err := archive.Create("keepermod.json")
	if err == nil {
		_, err = manifestWriter.Write(manifestData)
	}
	for _, file := range files {
		if err != nil {
			break
		}
		relative, _ := filepath.Rel(sourceFolder, file)
		writer, createErr := archive.Create(filepath.ToSlash(relative))
		if createErr != nil {
			err = createErr
			break
		}
		input, openErr := os.Open(file)
		if openErr != nil {
			err = openErr
			break
		}
		_, err = io.Copy(writer, input)
		input.Close()
	}
	closeZipErr := archive.Close()
	closeFileErr := out.Close()
	if err == nil {
		err = closeZipErr
	}
	if err == nil {
		err = closeFileErr
	}
	if err != nil {
		_ = os.Remove(temporary)
		return err
	}
	_ = os.Remove(outputZip)
	return os.Rename(temporary, outputZip)
}
