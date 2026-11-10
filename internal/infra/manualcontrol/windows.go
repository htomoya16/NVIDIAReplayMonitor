//go:build windows

package manualcontrol

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func DefaultPaths() (logPath, statePath string, err error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(dir, "NVIDIA Corporation", "NVIDIA Overlay", "console.log"),
		filepath.Join(dir, "NVIDIAReplayMonitor", "manual-control.json"), nil
}

func openLog(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

func fileIdentity(f *os.File) (string, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x:%x:%x:%x:%x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow,
		info.CreationTime.HighDateTime, info.CreationTime.LowDateTime), nil
}
