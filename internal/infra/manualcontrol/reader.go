package manualcontrol

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

const maxLogBytes = 8 << 20

var errBacklog = errors.New("操作ログの未読量が多いため、読み取りを継続しています")

func (c *Control) readLogs(ctx context.Context, state *State) error {
	seen := make(map[string]checkpoint)
	budget := int64(maxLogBytes)
	// NVIDIA moves console.log to console.log.bak. Read the old file first,
	// using its identity rather than its pathname to avoid replaying operations.
	for _, path := range []string{c.logPath + ".bak", c.logPath} {
		f, err := openLog(path)
		if errors.Is(err, os.ErrNotExist) && path != c.logPath {
			continue
		}
		if err != nil {
			return fmt.Errorf("操作ログを開けません: %w", err)
		}
		id, err := fileIdentity(f)
		if err != nil {
			f.Close()
			return err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		cursor, known := state.Files[id]
		if !state.Initialized {
			// First installation starts at EOF: historical manual off commands
			// must not suspend a newly installed monitor.
			cursor.Offset = info.Size()
			known = true
		} else if known && (info.Size() < cursor.Offset || !anchorMatches(f, cursor)) {
			cursor = checkpoint{}
		} else if !known {
			cursor = checkpoint{}
		}
		err = c.readFile(ctx, f, state, &cursor, &budget)
		if err == nil || errors.Is(err, errBacklog) {
			var anchorErr error
			cursor.Anchor, anchorErr = readAnchor(f, cursor.Offset)
			if anchorErr != nil {
				err = anchorErr
			}
		}
		// Verify the current pathname still points at the file we read. If it
		// rotated mid-read, skip capture and take a fresh snapshot next cycle.
		if err == nil && path == c.logPath {
			var current os.FileInfo
			current, err = os.Stat(path)
			if err == nil && !os.SameFile(info, current) {
				err = errors.New("操作ログの切替中です")
			}
		}
		f.Close()
		if errors.Is(err, errBacklog) {
			// Commit only complete lines, then continue next cycle without
			// allowing capture while user operations remain unread.
			for key, value := range seen {
				state.Files[key] = value
			}
			state.Files[id] = cursor
			return err
		}
		if err != nil {
			return err
		}
		seen[id] = cursor
	}
	state.Files = seen
	state.Initialized = true
	return nil
}

func (c *Control) readFile(ctx context.Context, f *os.File, state *State, cursor *checkpoint, budget *int64) error {
	if _, err := f.Seek(cursor.Offset, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := r.ReadSlice('\n')
		if errors.Is(err, io.EOF) {
			// An unfinished line remains unread until NVIDIA writes its newline.
			return nil
		}
		if err != nil {
			return fmt.Errorf("操作ログを読めません: %w", err)
		}
		*budget -= int64(len(line))
		if *budget < 0 {
			return errBacklog
		}
		cursor.Offset += int64(len(line))
		c.observe(state, cursor.Parser.line(string(line)))
	}
}

func readAnchor(f *os.File, offset int64) (string, error) {
	n := min(offset, 64)
	data := make([]byte, n)
	_, err := f.ReadAt(data, offset-n)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), err
}

func anchorMatches(f *os.File, cursor checkpoint) bool {
	anchor, err := readAnchor(f, cursor.Offset)
	return err == nil && anchor == cursor.Anchor
}
