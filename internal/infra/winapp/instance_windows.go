package winapp

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ErrAlreadyRunning = errors.New("監視はすでに起動しています")

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
	result, err := windows.WaitForSingleObject(done, uint32((30*time.Second)/time.Millisecond))
	if err != nil {
		return false, err
	}
	if result != windows.WAIT_OBJECT_0 {
		return false, errors.New("30秒以内に監視の終了を確認できませんでした")
	}
	return true, nil
}
