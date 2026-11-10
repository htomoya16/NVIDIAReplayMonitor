package manualcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
)

const stateVersion = 1

type checkpoint struct {
	Offset int64  `json:"offset"`
	Anchor string `json:"anchor,omitempty"`
	Parser parser `json:"parser"`
}

type State struct {
	Version     int                   `json:"version"`
	Initialized bool                  `json:"initialized"`
	Paused      bool                  `json:"paused"`
	Last        *Operation            `json:"last_operation,omitempty"`
	Files       map[string]checkpoint `json:"files"`
	ReadError   string                `json:"read_error,omitempty"`
}

// Control is owned by the single monitor. Diagnostics read its saved snapshot
// without advancing checkpoints or applying user operations.
type Control struct {
	logPath, statePath string
}

func New(logPath, statePath string) *Control {
	return &Control{logPath: logPath, statePath: statePath}
}

func ReadState(path string) (State, error) {
	var state State
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err = json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if state.Version != stateVersion || !state.Initialized || state.Files == nil {
		return state, errors.New("手動操作の保存情報が不正です")
	}
	for _, cursor := range state.Files {
		if cursor.Offset < 0 {
			return state, errors.New("操作ログの読み取り位置が不正です")
		}
	}
	return state, nil
}

func (c *Control) AllowRestore(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	previous, err := ReadState(c.statePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if errors.Is(err, os.ErrNotExist) {
		previous = State{Version: stateVersion, Files: make(map[string]checkpoint)}
	}
	// Work on a separate snapshot. Never persist partial operations from a
	// failed read, and never enable if a checkpoint cannot be saved.
	next := previous
	next.Files = maps.Clone(previous.Files)
	readErr := c.readLogs(ctx, &next)
	if readErr != nil {
		if errors.Is(readErr, errBacklog) {
			next.ReadError = readErr.Error()
			if err := c.save(next); err != nil {
				return false, err
			}
			return false, readErr
		}
		// Retain checkpoints and intent on errors. The next cycle retries the
		// same bytes instead of silently skipping an unread user command.
		if previous.Initialized && previous.ReadError != readErr.Error() {
			previous.ReadError = readErr.Error()
			if err := c.save(previous); err != nil {
				return false, err
			}
		}
		return false, readErr
	}
	next.ReadError = ""
	if !reflect.DeepEqual(previous, next) {
		if err := c.save(next); err != nil {
			return false, err
		}
	}
	return !next.Paused, nil
}

func RemoveState(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (c *Control) save(state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(c.statePath), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.statePath), ".manual-control-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(name, c.statePath); err != nil {
		return fmt.Errorf("手動操作の保存に失敗: %w", err)
	}
	return nil
}

func (c *Control) observe(state *State, op *Operation) {
	if op == nil || (state.Last != nil && op.Time <= state.Last.Time) {
		return
	}
	state.Paused = !op.Enabled
	state.Last = op
}
