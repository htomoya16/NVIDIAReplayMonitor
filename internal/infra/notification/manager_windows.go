package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/syndtr/goleveldb/leveldb/storage"
	"golang.org/x/sys/windows"
)

const databaseFolder = "https_nvfile_0.indexeddb.leveldb"

type savedState struct {
	Version  int      `json:"version"`
	Original *bool    `json:"original"`
	Phase    string   `json:"phase"`
	Backups  []string `json:"backups"`
}

type Manager struct{ databaseDir, stateDir string }

func DefaultManager() (*Manager, error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return nil, err
	}
	return &Manager{filepath.Join(dir, "NVIDIA Corporation", "NVIDIA Overlay", "CefCache", "Default", "IndexedDB", databaseFolder), filepath.Join(dir, "NVIDIAReplayMonitor")}, nil
}
func (m *Manager) StatePath() string { return filepath.Join(m.stateDir, "warning-state.json") }
func (m *Manager) HasState() (bool, error) {
	_, err := os.Stat(m.StatePath())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (m *Manager) Configured() (bool, error) {
	state, err := m.readState()
	return state != nil && (state.Phase == "applied" || state.Phase == "unchanged"), err
}

func (m *Manager) NeedsOverlayForRestore() (bool, error) {
	state, err := m.readState()
	return state != nil && state.Phase != "unchanged" && state.Phase != "restored", err
}

func (m *Manager) Apply() error   { return m.update(false) }
func (m *Manager) Restore() error { return m.update(true) }

func (m *Manager) readState() (*savedState, error) {
	b, err := os.ReadFile(m.StatePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state savedState
	if len(b) > 64*1024 || json.Unmarshal(b, &state) != nil || state.Version != 1 || !strings.Contains("|prepared|applied|restored|unchanged|", "|"+state.Phase+"|") {
		return nil, errors.New("通知設定の復元情報が不正です。バックアップを残して処理を中止しました")
	}
	for _, p := range state.Backups {
		if !m.isBackupPath(p) {
			return nil, errors.New("通知設定のバックアップ先が不正です")
		}
	}
	return &state, nil
}
func (m *Manager) isBackupPath(p string) bool {
	return filepath.IsAbs(p) && strings.EqualFold(filepath.Clean(filepath.Dir(p)), filepath.Clean(filepath.Dir(m.databaseDir))) && strings.HasPrefix(filepath.Base(p), ".nrm-db-")
}
func (m *Manager) saveState(state *savedState) error {
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.stateDir, "warning-state-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, writeErr := f.Write(b)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return os.Rename(name, m.StatePath())
}

func (m *Manager) update(restore bool) (err error) {
	if err = os.MkdirAll(m.stateDir, 0700); err != nil {
		return err
	}
	name, _ := windows.UTF16PtrFromString(filepath.Join(m.stateDir, "warning-operation.lock"))
	lock, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return fmt.Errorf("他の設定操作が実行中か、復元情報を開けません: %w", err)
	}
	defer windows.CloseHandle(lock)
	state, err := m.readState()
	if err != nil {
		return err
	}
	if restore && state == nil {
		return nil
	}
	if !restore && state != nil && state.Phase == "unchanged" {
		return nil
	}
	// Recover the one rename window in an interrupted directory replacement.
	if state != nil && len(state.Backups) > 0 {
		if _, e := os.Stat(m.databaseDir); errors.Is(e, os.ErrNotExist) {
			if e = os.Rename(state.Backups[len(state.Backups)-1], m.databaseDir); e != nil {
				return fmt.Errorf("中断したDBの復旧が必要です: %w", e)
			}
		}
	}
	if restore && (state.Phase == "restored" || state.Phase == "unchanged") {
		return m.finishRestore(state)
	}
	// A read-only LevelDB storage lock prevents the actual CEF database being opened
	// for writing. All LevelDB recovery, validation and edits happen on a copy.
	live, err := storage.OpenFile(m.databaseDir, true)
	if err != nil {
		return fmt.Errorf("通知DBをロックできません。NVIDIAオーバーレイをオフにして再実行してください: %w", err)
	}
	liveClosed := false
	defer func() {
		if !liveClosed {
			live.Close()
		}
	}()
	stage, err := os.MkdirTemp(filepath.Dir(m.databaseDir), ".nrm-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = copyDatabase(m.databaseDir, stage); err != nil {
		return err
	}
	db, err := openDatabase(stage)
	if err != nil {
		return fmt.Errorf("通知DBの検証に失敗しました。実設定は変更していません: %w", err)
	}
	defer db.db.Close()
	current, err := db.readBool(warningKey)
	if err != nil {
		return err
	}
	muted := false
	target := &muted
	if restore {
		target = state.Original
		if !sameBool(current, target) && !sameBool(current, &muted) {
			return errors.New("警告設定が適用後に変更されています。復元情報を残して処理を中止しました")
		}
	} else if state == nil {
		if sameBool(current, &muted) {
			// Remember successful initialization without claiming somebody else's
			// setting. Uninstall removes this marker without touching the DB.
			return m.saveState(&savedState{Version: 1, Original: current, Phase: "unchanged"})
		} // Already muted by somebody else: do not claim ownership.
		state = &savedState{Version: 1, Original: current, Phase: "prepared"}
	} else if !sameBool(current, &muted) && !sameBool(current, state.Original) {
		return errors.New("保存後に警告設定が変更されています。元の値を上書きしません")
	}
	if sameBool(current, target) {
		if restore {
			state.Phase = "restored"
			if err = m.saveState(state); err != nil {
				return err
			}
			return m.finishRestore(state)
		}
		state.Phase = "applied"
		return m.saveState(state)
	}
	if err = db.writeWarning(target); err != nil {
		return err
	}
	if err = db.db.Close(); err != nil {
		return err
	}
	backup, err := os.MkdirTemp(filepath.Dir(m.databaseDir), ".nrm-db-")
	if err != nil {
		return err
	}
	if err = os.Remove(backup); err != nil {
		return err
	}
	state.Backups = append(state.Backups, backup)
	if err = m.saveState(state); err != nil {
		return err
	}
	if err = live.Close(); err != nil {
		return err
	}
	liveClosed = true
	if err = os.Rename(m.databaseDir, backup); err != nil {
		return fmt.Errorf("Overlayが再起動したかDBが使用中です。実設定の反映を中止しました: %w", err)
	}
	if err = os.Rename(stage, m.databaseDir); err != nil {
		return errors.Join(err, os.Rename(backup, m.databaseDir))
	}
	state.Phase = "applied"
	if restore {
		state.Phase = "restored"
	}
	if err = m.saveState(state); err != nil {
		return fmt.Errorf("DB反映後の復元情報保存に失敗しました。再実行してください: %w", err)
	}
	if restore {
		return m.finishRestore(state)
	}
	return nil
}

func (m *Manager) finishRestore(state *savedState) error {
	for _, backup := range state.Backups {
		if !m.isBackupPath(backup) {
			return errors.New("不正なバックアップ先です")
		}
		if err := os.RemoveAll(backup); err != nil {
			return err
		}
	}
	return os.Remove(m.StatePath())
}
func copyDatabase(src, dst string) error {
	files, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range files {
		if entry.Name() == "LOCK" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("DB内に通常ファイル以外の項目があります")
		}
		in, err := os.Open(filepath.Join(src, entry.Name()))
		if err != nil {
			return err
		}
		out, err := os.OpenFile(filepath.Join(dst, entry.Name()), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return err
		}
		_, e := io.Copy(out, in)
		err = errors.Join(e, out.Sync(), out.Close(), in.Close())
		if err != nil {
			return err
		}
	}
	return nil
}
