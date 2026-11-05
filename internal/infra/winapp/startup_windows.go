package winapp

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const ShortcutName = "NVIDIAReplayMonitor.lnk"

func StartupShortcutPath() (string, error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_Startup, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ShortcutName), nil
}

// RemoveStartupShortcut only removes the single filename documented in the README.
// It never follows the link or deletes its target.
func RemoveStartupShortcut() (bool, error) {
	path, err := StartupShortcutPath()
	if err != nil {
		return false, err
	}
	return removeShortcutFile(path)
}

func removeShortcutFile(path string) (bool, error) {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func WriteDoctorReport(report string) (string, error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "NVIDIAReplayMonitor")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "doctor.txt")
	if err := os.WriteFile(path, []byte(report), 0600); err != nil {
		return "", err
	}
	return path, nil
}
