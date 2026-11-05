package notification

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
	"github.com/syndtr/goleveldb/leveldb/util"
)

func fixture(t *testing.T, original *bool) *Manager {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, databaseFolder)
	db, err := leveldb.OpenFile(dir, &opt.Options{Comparer: new(idbComparer)})
	if err != nil {
		t.Fatal(err)
	}
	put := func(k, v []byte) {
		if err := db.Put(k, v, nil); err != nil {
			t.Fatal(err)
		}
	}
	put(append(prefix(0, 0, 0), 0), []byte{5})
	put(append(prefix(0, 0, 0), 2), []byte{21, 0, 0, 0, 15})
	name := append(prefix(0, 0, 0), 201)
	name = append(name, encodeText("https://nvfile")...)
	name = append(name, encodeText("shareclient")...)
	put(name, []byte{3})
	put(append(prefix(3, 0, 0), 50, 5, 0), encodeText("IgoNotifications")[1:])
	put(append(prefix(3, 0, 0), 50, 5, 4), []byte{10})
	put(append(prefix(3, 0, 0), 50, 5, 5), []byte{30})
	global := append(prefix(3, 5, 1), 1)
	global = append(global, encodeText("GlobalToggle")...)
	value := append([]byte{1}, boolHeader...)
	put(global, append(value, 'T'))
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	m := &Manager{dir, filepath.Join(root, "state")}
	if original != nil {
		db, err := openDatabase(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.writeWarning(original); err != nil {
			t.Fatal(err)
		}
		db.db.Close()
	}
	return m
}
func readWarning(t *testing.T, m *Manager) *bool {
	t.Helper()
	db, err := openDatabase(m.databaseDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.db.Close()
	value, err := db.readBool(warningKey)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func TestNotificationOwnershipAndRestoration(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name     string
		original *bool
		owned    bool
	}{{"absent", nil, true}, {"enabled", &on, true}, {"already-muted", &off, false}} {
		t.Run(tc.name, func(t *testing.T) {
			m := fixture(t, tc.original)
			if err := m.Apply(); err != nil {
				t.Fatal(err)
			}
			if value := readWarning(t, m); !sameBool(value, &off) {
				t.Fatalf("not muted: %v", value)
			}
			owned, err := m.NeedsOverlayForRestore()
			if err != nil || owned != tc.owned {
				t.Fatalf("ownership=%v err=%v", owned, err)
			}
			if err = m.Apply(); err != nil {
				t.Fatal("repeated apply:", err)
			}
			configured, err := m.Configured()
			if err != nil || !configured {
				t.Fatalf("not initialized: %v %v", configured, err)
			}
			if owned, err = m.NeedsOverlayForRestore(); err != nil || owned != tc.owned {
				t.Fatalf("repeated apply changed ownership: %v %v", owned, err)
			}
			// A preference changed after setup must survive uninstall.
			db, err := openDatabase(m.databaseDir)
			if err != nil {
				t.Fatal(err)
			}
			other := append(prefix(3, 8, 1), 1)
			other = append(other, encodeText("new-preference")...)
			if err = db.db.Put(other, []byte("changed-after-setup"), nil); err != nil {
				t.Fatal(err)
			}
			db.db.Close()
			if err = m.Restore(); err != nil {
				t.Fatal(err)
			}
			if value := readWarning(t, m); !sameBool(value, tc.original) {
				t.Fatalf("original not restored: %v", value)
			}
			db, err = openDatabase(m.databaseDir)
			if err != nil {
				t.Fatal(err)
			}
			defer db.db.Close()
			value, err := db.db.Get(other, nil)
			if err != nil || string(value) != "changed-after-setup" {
				t.Fatalf("other setting lost: %q %v", value, err)
			}
			owned, err = m.HasState()
			if err != nil || owned {
				t.Fatalf("state not cleaned: %v %v", owned, err)
			}
			if err = m.Restore(); err != nil {
				t.Fatal("repeated restore:", err)
			}
		})
	}
}
func TestRestoreRefusesChangedWarningAndRetainsBackup(t *testing.T) {
	m := fixture(t, nil)
	if err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(m.databaseDir)
	if err != nil {
		t.Fatal(err)
	}
	on := true
	if err = db.writeWarning(&on); err != nil {
		t.Fatal(err)
	}
	db.db.Close()
	if err = m.Restore(); err == nil {
		t.Fatal("changed setting overwritten")
	}
	if exists, _ := m.HasState(); !exists {
		t.Fatal("backup lost")
	}
	if value := readWarning(t, m); !sameBool(value, &on) {
		t.Fatal("setting changed")
	}
}
func TestBusyDatabaseIsNotChanged(t *testing.T) {
	m := fixture(t, nil)
	locked, err := storage.OpenFile(m.databaseDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Apply(); err == nil {
		t.Fatal("busy database accepted")
	}
	locked.Close()
	if readWarning(t, m) != nil {
		t.Fatal("busy database changed")
	}
	if exists, _ := m.HasState(); exists {
		t.Fatal("ownership recorded before change")
	}
}
func TestUnsupportedFormatIsNotInstalled(t *testing.T) {
	m := fixture(t, nil)
	db, err := openDatabase(m.databaseDir)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.db.Put(append(prefix(0, 0, 0), 0), []byte{99}, nil); err != nil {
		t.Fatal(err)
	}
	db.db.Close()
	if err = m.Apply(); err == nil {
		t.Fatal("unsupported version accepted")
	}
	if exists, _ := m.HasState(); exists {
		t.Fatal("unsupported version claimed")
	}
}
func TestChromiumCopyCompatibility(t *testing.T) {
	src := os.Getenv("NRM_NOTIFICATION_FIXTURE_DIR")
	if src == "" {
		t.Skip("optional local CEF fixture")
	}
	root := t.TempDir()
	dir := filepath.Join(root, databaseFolder)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyDatabase(src, dir); err != nil {
		t.Fatal(err)
	}
	db, err := openDatabase(dir)
	if err != nil {
		t.Fatal(err)
	}
	before, err := db.readBool(warningKey)
	if err != nil {
		t.Fatal(err)
	}
	// The live machine may already be muted by the experiment. On this COPY,
	// start from an absent key so both apply and restore perform actual writes.
	if before != nil && !*before {
		if err = db.writeWarning(nil); err != nil {
			t.Fatal(err)
		}
		before = nil
	}
	db.db.Close()
	m := &Manager{dir, filepath.Join(root, "state")}
	if err = m.Apply(); err != nil {
		t.Fatal(err)
	}
	if export := os.Getenv("NRM_NOTIFICATION_EXPORT_DIR"); export != "" {
		out := filepath.Join(export, "muted", "Default", "IndexedDB", databaseFolder)
		if err = os.MkdirAll(out, 0700); err != nil {
			t.Fatal(err)
		}
		if err = copyDatabase(m.databaseDir, out); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.Restore(); err != nil {
		t.Fatal(err)
	}
	if export := os.Getenv("NRM_NOTIFICATION_EXPORT_DIR"); export != "" {
		out := filepath.Join(export, "restored", "Default", "IndexedDB", databaseFolder)
		if err = os.MkdirAll(out, 0700); err != nil {
			t.Fatal(err)
		}
		if err = copyDatabase(m.databaseDir, out); err != nil {
			t.Fatal(err)
		}
	}
	if !sameBool(before, readWarning(t, m)) {
		t.Fatal("CEF value not restored")
	}
}
func TestIndexedDBOrdering(t *testing.T) {
	key := func(payload []byte) []byte { return append(prefix(3, 5, 1), payload...) }
	text := func(s string) []byte { return key(append([]byte{1}, encodeText(s)...)) }
	number := func(v uint64) []byte { return key(binary.LittleEndian.AppendUint64([]byte{3}, v)) }
	c := new(idbComparer)
	if c.Compare(text("z"), text("aa")) <= 0 {
		t.Fatal("string length incorrectly determines order")
	}
	if c.Compare(number(0xbff0000000000000), number(0x3ff0000000000000)) >= 0 {
		t.Fatal("numeric byte order used")
	}
	if c.Compare(append(prefix(3, 0, 0), 50, 128, 1, 0), append(prefix(3, 0, 0), 50, 255, 0)) >= 0 {
		t.Fatal("metadata varint byte order used")
	}
	if !bytes.Equal(encodeText("通知")[1:], []byte{0x90, 0x1a, 0x77, 0xe5}) {
		t.Fatal("UTF16BE encoding")
	}
}

func TestJournalReplayPreservesTableAndJournalRecords(t *testing.T) {
	m := fixture(t, nil)
	db, err := openDatabase(m.databaseDir)
	if err != nil {
		t.Fatal(err)
	}
	oldKey := append(prefix(3, 8, 1), 1)
	oldKey = append(oldKey, encodeText("table-value")...)
	newKey := append(prefix(3, 8, 1), 1)
	newKey = append(newKey, encodeText("journal-value")...)
	if err = db.db.Put(oldKey, []byte("before-flush"), nil); err != nil {
		t.Fatal(err)
	}
	if err = db.db.CompactRange(util.Range{}); err != nil {
		t.Fatal(err)
	}
	if err = db.db.Put(newKey, []byte("after-flush"), &opt.WriteOptions{Sync: true}); err != nil {
		t.Fatal(err)
	}
	db.db.Close()
	current, err := os.ReadFile(filepath.Join(m.databaseDir, "CURRENT"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(m.databaseDir, string(bytes.TrimSpace(current)))
	records, err := readJournal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce Chromium's manifest sequence ahead of its still-active WAL.
	records = append(records, binary.AppendUvarint([]byte{4}, 100000))
	if err = writeJournal(manifest, records); err != nil {
		t.Fatal(err)
	}
	db, err = openDatabase(m.databaseDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.db.Close()
	for _, pair := range []struct {
		key   []byte
		value string
	}{{oldKey, "before-flush"}, {newKey, "after-flush"}} {
		v, err := db.db.Get(pair.key, nil)
		if err != nil || string(v) != pair.value {
			t.Fatalf("data lost: %q %v", v, err)
		}
	}
}
