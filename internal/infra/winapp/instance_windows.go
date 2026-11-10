package winapp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ErrAlreadyRunning = errors.New("監視はすでに起動しています")
var ErrMaintenanceBusy = errors.New("設定変更またはアンインストールを実行中です。完了後に再実行してください")

// The same gate covers setup, uninstall and starting a new monitor, so a new
// monitor cannot enable capture between the idle check and Overlay shutdown.
func AcquireMaintenance() (func(), error) {
	name, err := objectName("Maintenance")
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(h)
		return nil, ErrMaintenanceBusy
	}
	if err != nil {
		return nil, err
	}
	return func() { windows.CloseHandle(h) }, nil
}

func WaitMaintenance(ctx context.Context) (func(), error) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		release, err := AcquireMaintenance()
		if !errors.Is(err, ErrMaintenanceBusy) {
			return release, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("設定処理の終了待ちがタイムアウトしました。ファイルは削除していません: %w", ctx.Err())
		case <-tick.C:
		}
	}
}

type Instance struct {
	closeOnce sync.Once
	mutex     windows.Handle
	stop      windows.Handle
	done      windows.Handle
	watchDone chan struct{}
}

func objectName(suffix string) (*uint16, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return windows.UTF16PtrFromString(`Local\NVIDIAReplayMonitor.` + user.User.Sid.String() + "." + suffix)
}

func AcquireInstance() (*Instance, error) {
	if active, err := installerActive(); err != nil {
		return nil, err
	} else if active {
		return nil, ErrMaintenanceBusy
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return nil, err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, err := objectName("Instance")
	if err != nil {
		return nil, err
	}
	mutex, err := windows.CreateMutex(sa, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(mutex)
		return nil, ErrAlreadyRunning
	}
	if err != nil {
		return nil, err
	}
	instance := &Instance{mutex: mutex}
	// Close the race with an installer that acquired its gate after our
	// initial check and already looked for a running instance.
	if active, err := installerActive(); err != nil || active {
		instance.Close()
		if err != nil {
			return nil, err
		}
		return nil, ErrMaintenanceBusy
	}
	stopName, _ := objectName("Stop")
	instance.stop, err = windows.CreateEvent(sa, 1, 0, stopName)
	if err != nil {
		instance.Close()
		return nil, err
	}
	doneName, _ := objectName("Done")
	instance.done, err = windows.CreateEvent(sa, 1, 0, doneName)
	if err != nil {
		instance.Close()
		return nil, err
	}
	// A previous --stop command might briefly still hold an old event handle.
	_ = windows.ResetEvent(instance.stop)
	_ = windows.ResetEvent(instance.done)
	return instance, nil
}

func (i *Instance) WatchStop(cancel func()) {
	i.watchDone = make(chan struct{})
	go func() {
		defer close(i.watchDone)
		_, _ = windows.WaitForSingleObject(i.stop, windows.INFINITE)
		cancel()
	}()
}

func (i *Instance) Close() {
	i.closeOnce.Do(i.closeHandles)
}

func (i *Instance) closeHandles() {
	if i.stop != 0 {
		_ = windows.SetEvent(i.stop)
	}
	if i.watchDone != nil {
		<-i.watchDone
	}
	if i.done != 0 {
		_ = windows.SetEvent(i.done)
		_ = windows.CloseHandle(i.done)
	}
	if i.stop != 0 {
		_ = windows.CloseHandle(i.stop)
	}
	if i.mutex != 0 {
		_ = windows.CloseHandle(i.mutex)
	}
}

func IsRunning() (bool, error) {
	name, err := objectName("Instance")
	if err != nil {
		return false, err
	}
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_ = windows.CloseHandle(handle)
	return true, nil
}

// StopMonitor waits for the monitor's graceful teardown.
func StopMonitor() (bool, error) {
	return stopMonitor(30 * time.Second)
}

func StopForUninstall() (bool, error) {
	return stopMonitor(time.Minute)
}

func stopMonitor(timeout time.Duration) (bool, error) {
	running, err := IsRunning()
	if err != nil || !running {
		return false, err
	}
	stopName, _ := objectName("Stop")
	stop, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, stopName)
	if err != nil {
		return false, fmt.Errorf("停止通知を開けません（起動直後なら再実行してください）: %w", err)
	}
	defer windows.CloseHandle(stop)
	doneName, _ := objectName("Done")
	done, err := windows.OpenEvent(windows.SYNCHRONIZE, false, doneName)
	if err != nil {
		return false, fmt.Errorf("終了待機を開始できません: %w", err)
	}
	defer windows.CloseHandle(done)
	if err := windows.SetEvent(stop); err != nil {
		return false, err
	}
	result, err := windows.WaitForSingleObject(done, uint32(timeout/time.Millisecond))
	if err != nil {
		return false, err
	}
	if result != windows.WAIT_OBJECT_0 {
		return false, fmt.Errorf("%d秒以内に監視の終了を確認できませんでした", int(timeout/time.Second))
	}
	return true, nil
}
