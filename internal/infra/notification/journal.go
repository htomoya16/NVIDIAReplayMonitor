package notification

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/journal"
)

// Chromium may persist LastSequence while its active WAL still contains older
// batches. C++ LevelDB replays those batches; goleveldb rejects seq < LastSequence.
// On the COPY only, replay from zero and append an empty batch restoring the
// maximum sequence. No record is dropped, renumbered or read with checks disabled.
func prepareChromiumJournal(dir string) (err error) {
	defer func() {
		if recover() != nil {
			err = errEncoding
		}
	}()
	current, err := os.ReadFile(filepath.Join(dir, "CURRENT"))
	if err != nil {
		return err
	}
	manifest := strings.TrimSpace(string(current))
	if !strings.HasPrefix(manifest, "MANIFEST-") || filepath.Base(manifest) != manifest {
		return errEncoding
	}
	if _, err = strconv.ParseUint(strings.TrimPrefix(manifest, "MANIFEST-"), 10, 64); err != nil {
		return errEncoding
	}
	manifestPath := filepath.Join(dir, manifest)
	records, err := readJournal(manifestPath)
	if err != nil {
		return err
	}
	var maxSequence, logNumber, previousLog uint64
	for _, record := range records {
		d := decoder{record}
		for len(d.data) > 0 {
			switch d.variable() {
			case 1:
				d.binary()
			case 2:
				logNumber = d.variable()
			case 3:
				d.variable()
			case 4:
				if n := d.variable(); n > maxSequence {
					maxSequence = n
				}
			case 5:
				d.variable()
				d.binary()
			case 6:
				d.variable()
				d.variable()
			case 7:
				d.variable()
				d.variable()
				d.variable()
				d.binary()
				d.binary()
			case 9:
				previousLog = d.variable()
			default:
				return errEncoding
			}
		}
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var logs []string
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".log") {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSuffix(file.Name(), ".log"), 10, 64)
		if err != nil {
			return errEncoding
		}
		if n >= logNumber || n == previousLog {
			logs = append(logs, file.Name())
		}
	}
	if len(logs) == 0 {
		return nil
	}
	sort.Strings(logs)
	var end uint64
	for _, name := range logs {
		batches, err := readJournal(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		for _, batch := range batches {
			if len(batch) < 12 {
				return errEncoding
			}
			seq, count := binary.LittleEndian.Uint64(batch), uint64(binary.LittleEndian.Uint32(batch[8:]))
			if seq > 1<<56-2 || count > 1<<56-2-seq {
				return errEncoding
			}
			var decoded leveldb.Batch
			if err = decoded.Load(batch[12:]); err != nil {
				return err
			}
			if uint64(decoded.Len()) != count {
				return errEncoding
			}
			if seq < end {
				return errors.New("未対応の順序のLevelDBログです。実設定には反映しません")
			}
			end = seq + count
			if end > maxSequence {
				maxSequence = end
			}
		}
	}
	if maxSequence >= 1<<56-2 {
		return errEncoding
	}
	// A monotonic, zero-record batch preserves visibility of every SST entry even
	// if the manifest sequence is greater than the last active journal sequence.
	lastPath := filepath.Join(dir, logs[len(logs)-1])
	last, err := readJournal(lastPath)
	if err != nil {
		return err
	}
	empty := binary.LittleEndian.AppendUint64(nil, maxSequence+1)
	empty = binary.LittleEndian.AppendUint32(empty, 0)
	last = append(last, empty)
	if err = writeJournal(lastPath, last); err != nil {
		return err
	}
	records = append(records, []byte{4, 0}) // VersionEdit::kLastSequence = 0, for replay.
	return writeJournal(manifestPath, records)
}

func readJournal(path string) ([][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := journal.NewReader(f, nil, true, true)
	var records [][]byte
	size := 0
	for {
		record, err := r.Next()
		if err == io.EOF {
			return records, nil
		}
		if err != nil {
			return nil, fmt.Errorf("ログ検証: %w", err)
		}
		b, err := io.ReadAll(io.LimitReader(record, 128*1024*1024+1))
		if err != nil {
			return nil, err
		}
		size += len(b)
		if size > 128*1024*1024 {
			return nil, errors.New("検証できるログサイズを超えています")
		}
		records = append(records, bytes.Clone(b))
	}
}
func writeJournal(path string, records [][]byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "nrm-journal-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	w := journal.NewWriter(f)
	for _, record := range records {
		out, err := w.Next()
		if err != nil {
			f.Close()
			return err
		}
		if _, err = out.Write(record); err != nil {
			f.Close()
			return err
		}
	}
	err = errors.Join(w.Close(), f.Sync(), f.Close())
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
