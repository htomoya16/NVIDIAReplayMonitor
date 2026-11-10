package winapp

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type overlayProcess struct {
	pid     uint32
	path    string
	handle  windows.Handle
	started windows.Filetime
}
type restartIdentity struct {
	PID     uint32
	Started windows.Filetime
}
type restartProcessInfo struct {
	Process                  restartIdentity
	AppName                  [256]uint16
	ServiceName              [64]uint16
	AppType, Status, Session uint32
	Restartable              int32
}

// OverlayRestartWarning means the setting operation succeeded. Removal can
// complete while telling the user to check NVIDIA's restart separately.
type OverlayRestartWarning struct{ Cause error }

func (w *OverlayRestartWarning) Error() string {
	return "Overlay再起動後の確認に失敗しました。NVIDIA Appで状態を確認してください: " + w.Cause.Error()
}
func (w *OverlayRestartWarning) Unwrap() error { return w.Cause }

func operationResult(changeErr, restartErr error) error {
	if changeErr == nil && restartErr != nil {
		return &OverlayRestartWarning{Cause: restartErr}
	}
	return errors.Join(changeErr, restartErr)
}

// WithOverlayStopped uses Restart Manager without RmForceShutdown. Only the
// current user's verified Overlay root is registered; no services or file users.
// Failure to close or restart is returned to the caller, never hidden.
func WithOverlayStopped(ctx context.Context, idle, ready func(context.Context) error, change func() error) (err error) {
	process, err := findOverlay()
	if err != nil {
		return err
	}
	if process == nil {
		return change()
	}
	defer windows.CloseHandle(process.handle)
	if err = idle(ctx); err != nil {
		return err
	}
	dll := windows.NewLazySystemDLL("rstrtmgr.dll")
	var session uint32
	var key [33]uint16
	r, _, _ := dll.NewProc("RmStartSession").Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&key[0])))
	if r != 0 {
		return fmt.Errorf("正常終了セッションの開始: %w", syscall.Errno(r))
	}
	defer dll.NewProc("RmEndSession").Call(uintptr(session))
	identity := restartIdentity{process.pid, process.started}
	r, _, _ = dll.NewProc("RmRegisterResources").Call(uintptr(session), 0, 0, 1, uintptr(unsafe.Pointer(&identity)), 0, 0)
	if r != 0 {
		return fmt.Errorf("Overlayの正常終了準備: %w", syscall.Errno(r))
	}
	// Registering a process identity can only affect that process. Supply room
	// for it immediately, avoiding a size-query race with process termination.
	var needed, reasons uint32
	info := restartProcessInfo{}
	count := uint32(1)
	r, _, _ = dll.NewProc("RmGetList").Call(uintptr(session), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&reasons)))
	if err = validateRestartTarget(r, count, needed, reasons, info, identity); err != nil {
		return err
	}
	// Always attempt restart after shutdown, including partial shutdown failures.
	defer func() {
		dll.NewProc("RmRestart").Call(uintptr(session), 0, 0)
		restartErr := restartOverlay(process.path)
		if restartErr == nil && ctx.Err() == nil {
			readyContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			restartErr = ready(readyContext)
			cancel()
		}
		err = operationResult(err, restartErr)
	}()
	r, _, _ = dll.NewProc("RmShutdown").Call(uintptr(session), 0, 0)
	if r != 0 {
		return fmt.Errorf("Overlayを正常終了できませんでした。手動でオフにして再実行してください: %w", syscall.Errno(r))
	}
	wait, e := windows.WaitForSingleObject(process.handle, 10000)
	if e != nil || wait != windows.WAIT_OBJECT_0 {
		return errors.New("Overlayの正常終了を確認できません。設定は変更しません")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = waitForOverlayExit(ctx); err != nil {
		return err
	}
	return change()
}

func validateRestartTarget(code uintptr, count, needed, reasons uint32, info restartProcessInfo, expected restartIdentity) error {
	if code != 0 || count != 1 || needed > 1 || reasons != 0 {
		return fmt.Errorf("Overlayの終了対象を確認できません（Windowsエラー=%d、対象数=%d、必要数=%d、再起動理由=0x%x）。NVIDIA Appの起動完了後に再実行してください。続く場合はOverlayを手動でオフにしてください", code, count, needed, reasons)
	}
	// Unknown, main-window and other-window applications are all non-service
	// applications. Restart Manager may refuse them; shutdown still uses flags=0.
	if info.Process != expected || info.AppType > 2 {
		return fmt.Errorf("Overlayの終了対象が一致しません（PID=%d、種類=%d）。設定は変更していません", info.Process.PID, info.AppType)
	}
	return nil
}

func OverlayRunning() (bool, error) {
	entries, err := overlayEntries()
	return len(entries) != 0, err
}

func waitForOverlayExit(ctx context.Context) error {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		remaining, err := overlayEntries()
		if err != nil {
			return err
		}
		if len(remaining) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("Overlayの終了を確認できません（子プロセスの残存または自動再起動）。NVIDIA Appの設定から「NVIDIA オーバーレイ」をオフにして再実行してください")
		case <-tick.C:
		}
	}
}

func overlayEntries() ([]windows.ProcessEntry32, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	var session uint32
	if err = windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &session); err != nil {
		return nil, err
	}
	var entries []windows.ProcessEntry32
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), "NVIDIA Overlay.exe") {
			continue
		}
		var other uint32
		if e := windows.ProcessIdToSessionId(entry.ProcessID, &other); e != nil {
			return nil, e
		}
		if session == other {
			entries = append(entries, entry)
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return entries, nil
}

func findOverlay() (*overlayProcess, error) {
	entries, err := overlayEntries()
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	ids := make(map[uint32]bool)
	for _, e := range entries {
		ids[e.ProcessID] = true
	}
	var roots []uint32
	for _, e := range entries {
		if !ids[e.ParentProcessID] {
			roots = append(roots, e.ProcessID)
		}
	}
	if len(roots) != 1 {
		return nil, errors.New("Overlayの親プロセスを特定できません")
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, roots[0])
	if err != nil {
		return nil, err
	}
	p := &overlayProcess{pid: roots[0], handle: h}
	if err = verifyOverlay(p); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return p, nil
}
func verifyOverlay(p *overlayProcess) error {
	var token windows.Token
	if err := windows.OpenProcessToken(p.handle, windows.TOKEN_QUERY, &token); err != nil {
		return err
	}
	defer token.Close()
	owner, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	current, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	if !owner.User.Sid.Equals(current.User.Sid) {
		return errors.New("Overlayの利用者が一致しません")
	}
	var path [32768]uint16
	size := uint32(len(path))
	if err = windows.QueryFullProcessImageName(p.handle, 0, &path[0], &size); err != nil {
		return err
	}
	p.path = windows.UTF16ToString(path[:size])
	programFiles, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if err != nil {
		return err
	}
	expected := filepath.Join(programFiles, "NVIDIA Corporation", "NVIDIA App", "CEF", "NVIDIA Overlay.exe")
	if !strings.EqualFold(filepath.Clean(p.path), expected) {
		return errors.New("Overlayのインストール先を確認できません。手動でオフにしてください")
	}
	var exited, kernel, user windows.Filetime
	return windows.GetProcessTimes(p.handle, &p.started, &exited, &kernel, &user)
}
func restartOverlay(path string) error {
	p, err := findOverlay()
	if err != nil {
		return err
	}
	if p != nil {
		windows.CloseHandle(p.handle)
		return nil
	}
	cmd := exec.Command(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("Overlayを再起動できません。NVIDIA Appから手動でオンにしてください: %w", err)
	}
	cmd.Process.Release()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		p, err = findOverlay()
		if err != nil {
			return err
		}
		if p != nil {
			windows.CloseHandle(p.handle)
			return nil
		}
	}
	return errors.New("Overlayの再起動を確認できません。NVIDIA Appから手動でオンにしてください")
}
