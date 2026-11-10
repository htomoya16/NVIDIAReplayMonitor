package winapp

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Also used by installer/NVIDIAReplayMonitor.iss. The installer holds this
// session-local gate for the entire file transaction; lifecycle commands still
// work, but new monitors cannot start while files are replaced or removed.
const InstallerMutexName = `Local\NVIDIAReplayMonitor.Installer`

func installerActive() (bool, error) {
	name, err := windows.UTF16PtrFromString(InstallerMutexName)
	if err != nil {
		return false, err
	}
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	windows.CloseHandle(h)
	return true, nil
}
