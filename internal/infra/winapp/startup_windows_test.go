package winapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemovingShortcutDoesNotRemoveExecutable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "NVIDIAReplayMonitor.exe")
	shortcut := filepath.Join(dir, ShortcutName)
	for _, path := range []string{target, shortcut} {
		if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if removed, err := removeShortcutFile(shortcut); err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("executable was affected: %v", err)
	}
	if removed, err := removeShortcutFile(shortcut); err != nil || removed {
		t.Fatalf("second removal=%v err=%v", removed, err)
	}
}
