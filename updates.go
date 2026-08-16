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

func checksumForManager(data string) (string, error) {
	for _, line := range strings.Split(strings.ReplaceAll(data, "\r", ""), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if strings.EqualFold(filepath.Base(name), managerExecutableName) && sha256Pattern.MatchString(fields[0]) {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", errors.New("manager update package has no valid executable checksum")
}

func stageManagerUpdate(packagePath string) (string, error) {
	archive, err := zip.OpenReader(packagePath)
	if err != nil {
		return "", fmt.Errorf("select the KeeperLoader Windows artifact ZIP: %w", err)
	}
	defer archive.Close()
	if len(archive.File) > 64 {
		return "", errors.New("manager update package contains too many files")
	}
	var executableEntry, versionEntry, checksumEntry *zip.File
	seen := map[string]bool{}
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
		if expanded > 96*1024*1024 {
			return "", errors.New("manager update package exceeds the 96 MB expanded-size limit")
		}
		if strings.Contains(name, "/") || entry.FileInfo().IsDir() {
			continue
		}
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
	if compareVersion(parsedNew, parsedCurrent) <= 0 {
		return "", fmt.Errorf("manager update package is version %s; a version newer than %s is required", newVersion, loaderVersion)
	}
	checksumText, err := readZipText(checksumEntry, 64*1024)
	if err != nil {
		return "", err
	}
	expectedDigest, err := checksumForManager(checksumText)
	if err != nil {
		return "", err
	}
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		root = os.TempDir()
	}
	updateDirectory := filepath.Join(root, "KeeperLoader", "updates", newVersion+"-"+timestamp())
	if err = os.MkdirAll(updateDirectory, 0755); err != nil {
		return "", err
	}
	stagedExecutable := filepath.Join(updateDirectory, managerExecutableName)
	if err = extractZipFile(executableEntry, stagedExecutable, 64*1024*1024); err != nil {
		_ = os.RemoveAll(updateDirectory)
		return "", err
	}
	digest, err := fileSHA256(stagedExecutable)
	if err != nil || !strings.EqualFold(digest, expectedDigest) {
		_ = os.RemoveAll(updateDirectory)
		return "", errors.New("manager update executable failed its SHA-256 integrity check")
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
	_ = os.Remove(newTarget)
	_ = os.Remove(previousTarget)
	if err = copyFile(self, newTarget); err != nil {
		return err
	}
	if err = os.Rename(target, previousTarget); err != nil {
		_ = os.Remove(newTarget)
		return err
	}
	if err = os.Rename(newTarget, target); err != nil {
		_ = os.Rename(previousTarget, target)
		_ = os.Remove(newTarget)
		return err
	}
	command := exec.Command(target, "--update-complete", updateDirectory, strconv.Itoa(os.Getpid()))
	command.Dir = filepath.Dir(target)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err = command.Start(); err != nil {
		_ = os.Remove(target)
		_ = os.Rename(previousTarget, target)
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
