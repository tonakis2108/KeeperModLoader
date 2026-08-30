//go:build windows

package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lxn/walk"
	"golang.org/x/sys/windows"
)

const (
	managerExecutableName = "KeeperLoader-Manager.exe"
	managerVersionName    = "VERSION"
	managerChecksumsName  = "SHA256SUMS.txt"
)

func readZipText(entry *zip.File, limit int64) (string, error) {
	if entry == nil || int64(entry.UncompressedSize64) > limit {
		return "", errors.New("manager update package contains an invalid metadata file")
	}
	input, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer input.Close()
	data, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > limit {
		return "", errors.New("manager update metadata exceeds its size limit")
	}
	return string(data), nil
}

func extractZipFile(entry *zip.File, destination string, limit int64) error {
	if entry == nil || int64(entry.UncompressedSize64) > limit {
		return errors.New("manager update executable exceeds its size limit")
	}
	input, err := entry.Open()
	if err != nil {
		return err
	}
	defer input.Close()
	if err = os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, limit+1))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > limit {
		_ = os.Remove(destination)
		return errors.New("manager update executable exceeds its size limit")
	}
	return nil
}

func managerPackageChecksums(data string) (map[string]string, error) {
	result := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(data, "\r", ""), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !sha256Pattern.MatchString(fields[0]) {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		clean, err := normalizedArchivePath(strings.ReplaceAll(name, `\`, "/"))
		if err != nil {
			return nil, fmt.Errorf("manager checksum path rejected: %w", err)
		}
		result[strings.ToLower(clean)] = strings.ToLower(fields[0])
	}
	if _, ok := result[strings.ToLower(managerExecutableName)]; !ok {
		return nil, errors.New("manager update package has no valid executable checksum")
	}
	for _, name := range requiredRuntimePayloadFiles {
		if _, ok := result[strings.ToLower(runtimePayloadDirectory+"/"+name)]; !ok {
			return nil, fmt.Errorf("manager update package has no checksum for runtime/%s", name)
		}
	}
	return result, nil
}

func stageManagerUpdate(packagePath string) (string, error) {
	archive, err := zip.OpenReader(packagePath)
	if err != nil {
		return "", fmt.Errorf("select the KeeperLoader Windows artifact ZIP: %w", err)
	}
	defer archive.Close()
	if len(archive.File) > 256 {
		return "", errors.New("manager update package contains too many files")
	}
	var executableEntry, versionEntry, checksumEntry *zip.File
	seen := map[string]bool{}
	entries := map[string]*zip.File{}
	var expanded uint64
	for _, entry := range archive.File {
		name, pathErr := normalizedArchivePath(entry.Name)
		if pathErr != nil {
			return "", fmt.Errorf("manager update package rejected: %w", pathErr)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return "", fmt.Errorf("manager update package contains duplicate path %q", name)
		}
		seen[key] = true
		expanded += entry.UncompressedSize64
		if expanded > 192*1024*1024 {
			return "", errors.New("manager update package exceeds the 192 MB expanded-size limit")
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("manager update package contains symbolic link %q", name)
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		entries[key] = entry
		switch {
		case strings.EqualFold(name, managerExecutableName):
			executableEntry = entry
		case strings.EqualFold(name, managerVersionName):
			versionEntry = entry
		case strings.EqualFold(name, managerChecksumsName):
			checksumEntry = entry
		}
	}
	if executableEntry == nil || versionEntry == nil || checksumEntry == nil {
		return "", errors.New("manager update package must contain KeeperLoader-Manager.exe, VERSION, and SHA256SUMS.txt")
	}
	versionText, err := readZipText(versionEntry, 128)
	if err != nil {
		return "", err
	}
	newVersion := strings.TrimSpace(versionText)
	parsedNew, err := parseVersion(newVersion)
	if err != nil {
		return "", errors.New("manager update package VERSION is invalid")
	}
	parsedCurrent, _ := parseVersion(loaderVersion)
	comparison := compareVersion(parsedNew, parsedCurrent)
	if comparison < 0 {
		return "", fmt.Errorf("manager update package is version %s; a version newer than %s is required", newVersion, loaderVersion)
	}
	if comparison == 0 {
		currentPayload, payloadErr := runtimePayloadRoot()
		if payloadErr == nil {
			payloadErr = validateRuntimePayload(currentPayload)
		}
		if payloadErr == nil {
			return "", fmt.Errorf("KeeperLoader Manager %s and its runtime payload are already installed", loaderVersion)
		}
	}
	checksumText, err := readZipText(checksumEntry, 64*1024)
	if err != nil {
		return "", err
	}
	expectedDigests, err := managerPackageChecksums(checksumText)
	if err != nil {
		return "", err
	}
	for key := range entries {
		if strings.EqualFold(key, managerChecksumsName) {
			continue
		}
		if _, ok := expectedDigests[key]; !ok {
			return "", fmt.Errorf("manager update package contains undeclared file %q", entries[key].Name)
		}
	}
	for key := range expectedDigests {
		if _, ok := entries[key]; !ok {
			return "", fmt.Errorf("manager update package checksum references missing file %q", key)
		}
	}
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		root = os.TempDir()
	}
	updateDirectory := filepath.Join(root, "KeeperLoader", "updates", newVersion+"-"+timestamp())
	if err = os.MkdirAll(updateDirectory, 0755); err != nil {
		return "", err
	}
	for key, digest := range expectedDigests {
		entry := entries[key]
		destination := filepath.Join(updateDirectory, filepath.FromSlash(key))
		if err = extractZipFile(entry, destination, 64*1024*1024); err != nil {
			_ = os.RemoveAll(updateDirectory)
			return "", err
		}
		actual, hashErr := fileSHA256(destination)
		if hashErr != nil || !strings.EqualFold(actual, digest) {
			_ = os.RemoveAll(updateDirectory)
			return "", fmt.Errorf("manager update file failed its SHA-256 integrity check: %s", key)
		}
	}
	stagedExecutable := filepath.Join(updateDirectory, managerExecutableName)
	if err = validateRuntimePayloadForVersion(filepath.Join(updateDirectory, runtimePayloadDirectory), newVersion); err != nil {
		_ = os.RemoveAll(updateDirectory)
		return "", fmt.Errorf("manager update runtime rejected: %w", err)
	}
	currentExecutable, err := os.Executable()
	if err != nil {
		_ = os.RemoveAll(updateDirectory)
		return "", err
	}
	command := exec.Command(stagedExecutable, "--apply-update", currentExecutable, strconv.Itoa(os.Getpid()), updateDirectory)
	command.Dir = updateDirectory
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err = command.Start(); err != nil {
		_ = os.RemoveAll(updateDirectory)
		return "", fmt.Errorf("could not start the verified update: %w", err)
	}
	return newVersion, nil
}

func waitForPID(pid uint32, timeout time.Duration) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(handle)
	milliseconds := uint32(timeout / time.Millisecond)
	result, err := windows.WaitForSingleObject(handle, milliseconds)
	if err != nil {
		return err
	}
	if result == uint32(windows.WAIT_TIMEOUT) {
		return errors.New("timed out waiting for the previous manager process to close")
	}
	return nil
}

func applyManagerUpdate(target string, previousPID uint32, updateDirectory string) error {
	if err := waitForPID(previousPID, 2*time.Minute); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	target = filepath.Clean(target)
	if !fileExists(target) {
		return errors.New("the existing manager executable was not found")
	}
	newTarget := target + ".new"
	previousTarget := target + ".previous"
	targetRuntime := filepath.Join(filepath.Dir(target), runtimePayloadDirectory)
	newRuntime := targetRuntime + ".new"
	previousRuntime := targetRuntime + ".previous"
	_ = os.Remove(newTarget)
	_ = os.Remove(previousTarget)
	_ = os.RemoveAll(newRuntime)
	_ = os.RemoveAll(previousRuntime)
	if err = copyDirectory(filepath.Join(updateDirectory, runtimePayloadDirectory), newRuntime); err != nil {
		return err
	}
	if err = copyFile(self, newTarget); err != nil {
		_ = os.RemoveAll(newRuntime)
		return err
	}
	if dirExists(targetRuntime) {
		if err = os.Rename(targetRuntime, previousRuntime); err != nil {
			_ = os.Remove(newTarget)
			_ = os.RemoveAll(newRuntime)
			return err
		}
	}
	if err = os.Rename(newRuntime, targetRuntime); err != nil {
		if dirExists(previousRuntime) {
			_ = os.Rename(previousRuntime, targetRuntime)
		}
		_ = os.Remove(newTarget)
		return err
	}
	if err = os.Rename(target, previousTarget); err != nil {
		_ = os.RemoveAll(targetRuntime)
		if dirExists(previousRuntime) {
			_ = os.Rename(previousRuntime, targetRuntime)
		}
		_ = os.Remove(newTarget)
		return err
	}
	if err = os.Rename(newTarget, target); err != nil {
		_ = os.Rename(previousTarget, target)
		_ = os.RemoveAll(targetRuntime)
		if dirExists(previousRuntime) {
			_ = os.Rename(previousRuntime, targetRuntime)
		}
		_ = os.Remove(newTarget)
		return err
	}
	command := exec.Command(target, "--update-complete", updateDirectory, strconv.Itoa(os.Getpid()))
	command.Dir = filepath.Dir(target)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err = command.Start(); err != nil {
		_ = os.Remove(target)
		_ = os.Rename(previousTarget, target)
		_ = os.RemoveAll(targetRuntime)
		if dirExists(previousRuntime) {
			_ = os.Rename(previousRuntime, targetRuntime)
		}
		return err
	}
	return nil
}

func handleManagerUpdateMode() bool {
	if len(os.Args) < 2 {
		return false
	}
	switch os.Args[1] {
	case "--apply-update":
		if len(os.Args) != 5 {
			walk.MsgBox(nil, "KeeperLoader update failed", "Invalid update arguments.", walk.MsgBoxIconError)
			return true
		}
		pid, err := strconv.ParseUint(os.Args[3], 10, 32)
		if err == nil {
			err = applyManagerUpdate(os.Args[2], uint32(pid), os.Args[4])
		}
		if err != nil {
			walk.MsgBox(nil, "KeeperLoader update failed", err.Error(), walk.MsgBoxIconError)
		}
		return true
	case "--update-complete":
		if len(os.Args) == 4 {
			updateDirectory := filepath.Clean(os.Args[2])
			helperPID, parseErr := strconv.ParseUint(os.Args[3], 10, 32)
			if parseErr == nil {
				go func() {
					_ = waitForPID(uint32(helperPID), 30*time.Second)
					if current, executableErr := os.Executable(); executableErr == nil {
						_ = os.Remove(current + ".previous")
						_ = os.RemoveAll(filepath.Join(filepath.Dir(current), runtimePayloadDirectory) + ".previous")
					}
					_ = os.RemoveAll(updateDirectory)
				}()
			}
		}
		return false
	default:
		return false
	}
}
