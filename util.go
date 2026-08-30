package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const loaderVersion = "0.7.5"

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err = os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func copyDirectory(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("source directory is unavailable: %s", source)
	}
	return filepath.Walk(source, func(path string, entry os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not allowed in copied directories: %s", path)
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temporary := path + ".tmp-" + fmt.Sprint(time.Now().UnixNano())
	if err := os.WriteFile(temporary, data, mode); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(temporary, path)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func timestamp() string { return time.Now().Format("20060102-150405") }

func uniqueTimestampPath(root, name string) string {
	path := filepath.Join(root, name+"-"+timestamp())
	if !fileExists(path) && !dirExists(path) {
		return path
	}
	return fmt.Sprintf("%s-%d", path, time.Now().UnixNano()%100000000)
}

func isProcessRunning(executableName string) (bool, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafeSizeofProcessEntry32())}
	if err = windows.Process32First(snapshot, &entry); err != nil {
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return false, nil
		}
		return false, err
	}
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, executableName) {
			return true, nil
		}
		if err = windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return false, err
		}
	}
	return false, nil
}

// Kept in a helper so the initialization is accepted by older x/sys releases.
func unsafeSizeofProcessEntry32() uintptr {
	var entry windows.ProcessEntry32
	return uintptr(unsafe.Sizeof(entry))
}

func assertGameStopped(game *GameInfo) error {
	running, err := isProcessRunning(game.ExecutableName)
	if err != nil {
		return fmt.Errorf("could not check whether %s is running: %w", game.ProcessName, err)
	}
	if running {
		return fmt.Errorf("close %s before changing KeeperLoader or its mods", game.ProcessName)
	}
	return nil
}

var versionPattern = regexp.MustCompile(`^\d+\.\d+(?:\.\d+)?(?:\.\d+)?$`)

func parseVersion(value string) ([4]int, error) {
	var result [4]int
	if !versionPattern.MatchString(value) {
		return result, fmt.Errorf("invalid version %q", value)
	}
	parts := strings.Split(value, ".")
	for i, part := range parts {
		if _, err := fmt.Sscanf(part, "%d", &result[i]); err != nil {
			return result, err
		}
	}
	return result, nil
}

func compareVersion(a, b [4]int) int {
	for i := 0; i < 4; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func openExplorer(path string) error {
	if !dirExists(path) {
		return fmt.Errorf("folder does not exist: %s", path)
	}
	return exec.Command("explorer.exe", path).Start()
}

func launchGameThroughSteam(game *GameInfo) (string, error) {
	if game == nil {
		return "", errors.New("select a game first")
	}
	if game.SteamAppID == "" {
		game.SteamAppID = steamAppIDForGameDirectory(game.GameDirectory)
	}
	uri, err := steamRunURI(game.SteamAppID)
	if err != nil {
		return "", err
	}
	target, err := windows.UTF16PtrFromString(uri)
	if err != nil {
		return "", fmt.Errorf("could not prepare the Steam launch request: %w", err)
	}
	if err = windows.ShellExecute(windows.Handle(0), nil, target, nil, nil, 1); err != nil {
		return "", fmt.Errorf("Steam could not launch the selected game: %w", err)
	}
	return uri, nil
}

func persistentDataLocation(game *GameInfo) (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	root := filepath.Join(home, "AppData", "LocalLow")
	if !dirExists(root) {
		return "", "", errors.New("Unity's LocalLow persistent-data folder was not found")
	}
	if game.GameID == "graveyard-keeper" {
		known := filepath.Join(root, "Lazy Bear Games", "Graveyard Keeper")
		if dirExists(known) {
			return known, "Detected Graveyard Keeper save-data folder.", nil
		}
	}
	return root, "Opened Unity's LocalLow root; choose the game's publisher and product folder.", nil
}
