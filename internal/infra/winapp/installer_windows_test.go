package winapp

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestInstallerPreventsNewMonitorWithoutBlockingCommands(t *testing.T) {
	name, _ := windows.UTF16PtrFromString(InstallerMutexName)
	h, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		t.Skip("installer is running")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	instance, err := AcquireInstance()
	if instance != nil {
		instance.Close()
	}
	if !errors.Is(err, ErrMaintenanceBusy) {
		t.Fatalf("new instance: %v", err)
	}
	// Installer commands must be able to take Maintenance for restoration;
	// using the same gate for both would deadlock uninstall.
	release, err := AcquireMaintenance()
	if err != nil {
		t.Fatal(err)
	}
	release()
}
