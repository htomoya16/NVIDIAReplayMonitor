package manualcontrol

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ui(at, value string) string {
	return "2026-11-10 " + at + " INFO  IntantReplay  on toggle IR:  " + value + " \r\n"
}

func hotkey(at, command string) string {
	return "2026-11-10 " + at + " INFO  HotkeyService  Hotkey:  {\r\n" +
		"\t\"hotkey\": \"DVRToggle\",\r\n\t\"state\": \"Down\",\r\n}\r\n" +
		"2026-11-10 " + at + " INFO  ShadowPlayService  " + command + " \r\n"
}

func appendLog(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(content)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T) (*Control, string, string) {
	t.Helper()
	dir := t.TempDir()
	logPath, statePath := filepath.Join(dir, "console.log"), filepath.Join(dir, "manual-control.json")
	appendLog(t, logPath, ui("19:00:00.000", "false"))
	c := New(logPath, statePath)
	checkAllowed(t, c, true)
	return c, logPath, statePath
}

func checkAllowed(t *testing.T, c *Control, want bool) {
	t.Helper()
	got, err := c.AllowRestore(context.Background())
	if err != nil || got != want {
		t.Fatalf("allowed=%v err=%v want=%v", got, err, want)
	}
}

func TestManualOperationsSurviveRestartAndRotation(t *testing.T) {
	c, path, statePath := setup(t)
	appendLog(t, path, ui("19:01:00.000", "false"))
	checkAllowed(t, c, false)
	// A previously processed off survives a monitor restart. An on that
	// happened while it was stopped is recovered from the unread bytes.
	c = New(path, statePath)
	checkAllowed(t, c, false)
	appendLog(t, path, hotkey("19:01:06.000", "IR Enabled"))
	if err := os.Rename(path, path+".bak"); err != nil {
		t.Fatal(err)
	}
	appendLog(t, path, "2026-11-10 19:01:07.000 INFO  ShadowPlayService  IR Disabled \r\n")
	checkAllowed(t, c, true)
	checkAllowed(t, New(path, statePath), true)
	state, err := ReadState(statePath)
	if err != nil || state.Last.Source != "hotkey" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestPartialLineAndHotkeyAcrossPolls(t *testing.T) {
	c, path, _ := setup(t)
	line := ui("19:01:00.000", "false")
	appendLog(t, path, strings.TrimSuffix(line, "\r\n"))
	checkAllowed(t, c, true)
	appendLog(t, path, "\r\n")
	checkAllowed(t, c, false)
	lines := hotkey("19:01:06.000", "IR Enabled")
	// Save the shortcut body before its command entry arrives.
	index := strings.LastIndex(lines, "2026-11-10")
	appendLog(t, path, lines[:index])
	checkAllowed(t, c, false)
	appendLog(t, path, lines[index:])
	checkAllowed(t, c, true)
}

func TestTruncateAndMissingLogDoNotReplayOldIntent(t *testing.T) {
	c, path, _ := setup(t)
	appendLog(t, path, ui("19:01:00.000", "false")+ui("19:02:00.000", "true"))
	checkAllowed(t, c, true)
	if err := os.WriteFile(path, []byte(ui("19:01:00.000", "false")), 0600); err != nil {
		t.Fatal(err)
	}
	checkAllowed(t, c, true)
	appendLog(t, path, hotkey("19:03:00.000", "IR Disabled"))
	checkAllowed(t, c, false)
	if err := os.Rename(path, path+".bak"); err != nil {
		t.Fatal(err)
	}
	if allowed, err := c.AllowRestore(context.Background()); allowed || err == nil {
		t.Fatal("missing log must block restore")
	}
	appendLog(t, path, ui("19:04:00.000", "true"))
	checkAllowed(t, c, true)
}

func TestCorruptStateBlocksCapture(t *testing.T) {
	c, _, path := setup(t)
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if allowed, err := c.AllowRestore(context.Background()); allowed || err == nil {
		t.Fatal("corrupt state accepted")
	}
}

func TestCleanupRemovesOnlyOwnedState(t *testing.T) {
	_, _, path := setup(t)
	other := filepath.Join(filepath.Dir(path), "keep.txt")
	appendLog(t, other, "keep")
	if err := RemoveState(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("state remains: %v", err)
	}
	if err := RemoveState(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal(err)
	}
}

func TestSaveFailureBlocksRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "console.log")
	appendLog(t, path, "initial\n")
	blocker := filepath.Join(dir, "not-a-directory")
	appendLog(t, blocker, "file")
	c := New(path, filepath.Join(blocker, "manual-control.json"))
	if allow, err := c.AllowRestore(context.Background()); allow || err == nil {
		t.Fatal("save failure accepted")
	}
}

func TestRecordedOverlayOperations(t *testing.T) {
	path := os.Getenv("NRM_TEST_OVERLAY_LOG")
	if path == "" {
		t.Skip("local read-only verification of NVIDIA's actual log")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var p parser
	got := make(map[string]Operation)
	for _, line := range strings.Split(string(data), "\n") {
		if op := p.line(line); op != nil {
			got[op.Time] = *op
		}
	}
	for _, want := range []Operation{
		{Time: "2026-11-10 19:34:16.338", Enabled: false, Source: "overlay"},
		{Time: "2026-11-10 19:34:22.306", Enabled: true, Source: "overlay"},
		{Time: "2026-11-10 19:34:34.675", Enabled: false, Source: "hotkey"},
		{Time: "2026-11-10 19:34:40.127", Enabled: true, Source: "hotkey"},
	} {
		if got[want.Time] != want {
			t.Errorf("got=%+v want=%+v", got[want.Time], want)
		}
	}
	for at := range got {
		if at >= "2026-11-10 17:07:00.000" && at < "2026-11-10 17:23:00.000" {
			t.Errorf("automatic stop mistaken for user operation: %s", at)
		}
	}
}

func TestBacklogMakesProgressWithoutEnabling(t *testing.T) {
	c, path, _ := setup(t)
	appendLog(t, path, ui("19:01:00.000", "false")+strings.Repeat("unrelated log entry\n", maxLogBytes/15)+ui("19:02:00.000", "true"))
	if allowed, err := c.AllowRestore(context.Background()); allowed || err == nil {
		t.Fatal("unread backlog accepted")
	}
	checkAllowed(t, c, true)
}
