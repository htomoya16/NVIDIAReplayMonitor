package winapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticCleanupKeepsRecoveryAndUserFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"doctor.txt", "uninstall-error.txt", "warning-operation.lock", "warning-state.json", "keep.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeDiagnosticFiles(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"warning-state.json", "keep.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"doctor.txt", "uninstall-error.txt", "warning-operation.lock"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("not removed: %s %v", name, err)
		}
	}
}
