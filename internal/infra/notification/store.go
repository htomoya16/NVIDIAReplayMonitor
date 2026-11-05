package notification

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"math"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

const warningKey = "noteProtectedContentWarning"

// Only the serialization format verified with NVIDIA App 11.0.9.585 is writable.
// Unknown versions fail closed, including during uninstall; the backup is kept.
var boolHeader, _ = hex.DecodeString("ff15fe000000000000000000000000ff0f")

type database struct {
	db                  *leveldb.DB
	comparer            *idbComparer
	databaseID, storeID uint64
	records             map[string][]byte
}

func openDatabase(path string) (_ *database, err error) {
	var opened *leveldb.DB
	defer func() {
		if recover() != nil {
			err = errEncoding
		}
		if err != nil && opened != nil {
			opened.Close()
		}
	}()
	if err = prepareChromiumJournal(path); err != nil {
		return nil, err
	}
	c := new(idbComparer)
	db, err := leveldb.OpenFile(path, &opt.Options{Comparer: c, ErrorIfMissing: true, Strict: opt.StrictAll, WriteBuffer: 4 * 1024 * 1024})
	if err != nil {
		return nil, err
	}
	opened = db
	d := &database{db: db, comparer: c}
	d.records, err = d.snapshot()
	if err != nil {
		return nil, err
	}
	if littleInt(d.records[string(prefix(0, 0, 0))+string([]byte{0})]) != 5 {
		return nil, errEncoding
	}
	if littleInt(d.records[string(prefix(0, 0, 0))+string([]byte{2})]) != uint64(15)<<32|21 {
		return nil, errEncoding
	}
	for raw, value := range d.records {
		key, e := decodeKey([]byte(raw))
		if e != nil {
			return nil, e
		}
		if key.prefix == [3]uint64{} && key.parts[0].number == 50 {
			// Scope metadata beyond the global version denotes an unfinished transaction.
			if len(key.parts[1].text) != 1 || key.parts[1].text[0] != 0 {
				return nil, errors.New("IndexedDBに未完了の処理があります。NVIDIA Appを正常終了して再実行してください")
			}
		}
		if key.prefix == [3]uint64{} && key.parts[0].number == 201 && string(key.parts[2].text) == string(encodeText("shareclient")[1:]) {
			if d.databaseID != 0 {
				return nil, errEncoding
			}
			d.databaseID = littleInt(value)
		}
	}
	if d.databaseID == 0 {
		return nil, errors.New("Overlay設定DBが見つかりません")
	}
	for raw, value := range d.records {
		key, _ := decodeKey([]byte(raw))
		if key.prefix == [3]uint64{d.databaseID, 0, 0} && key.parts[0].number == 50 && key.parts[2].number == 0 && decodeText(value) == "IgoNotifications" {
			if d.storeID != 0 {
				return nil, errEncoding
			}
			d.storeID = key.parts[1].number
		}
	}
	if d.storeID == 0 {
		return nil, errors.New("通知設定が見つかりません")
	}
	for raw := range d.records {
		key, _ := decodeKey([]byte(raw))
		if key.prefix == [3]uint64{d.databaseID, 0, 0} && key.parts[0].number == 100 && key.parts[1].number == d.storeID {
			return nil, errors.New("未対応の通知設定インデックスです")
		}
	}
	if _, err = d.readBool("GlobalToggle"); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *database) snapshot() (map[string][]byte, error) {
	result := make(map[string][]byte)
	it := d.db.NewIterator(nil, &opt.ReadOptions{Strict: opt.StrictAll})
	defer it.Release()
	var size int
	for it.Next() {
		if _, err := decodeKey(it.Key()); err != nil {
			return nil, err
		}
		size += len(it.Key()) + len(it.Value())
		if size > 128*1024*1024 || len(result) > 100000 {
			return nil, errors.New("設定DBが検証上限を超えています")
		}
		result[string(it.Key())] = bytes.Clone(it.Value())
	}
	if err := it.Error(); err != nil {
		return nil, err
	}
	if d.comparer.invalid.Load() {
		return nil, errEncoding
	}
	return result, nil
}
func (d *database) key(index uint64, name string) []byte {
	b := append(prefix(d.databaseID, d.storeID, index), 1)
	return append(b, encodeText(name)...)
}
func decodeBool(value []byte) (_ *bool, err error) {
	if value == nil {
		return nil, nil
	}
	_, n := binary.Uvarint(value)
	if n <= 0 {
		return nil, errEncoding
	}
	v := value[n:]
	if len(v) != len(boolHeader)+1 || !bytes.Equal(v[:len(boolHeader)], boolHeader) {
		return nil, errEncoding
	}
	b := v[len(boolHeader)] == 'T'
	if !b && v[len(boolHeader)] != 'F' {
		return nil, errEncoding
	}
	return &b, nil
}
func (d *database) readBool(name string) (*bool, error) {
	return decodeBool(d.records[string(d.key(1, name))])
}

func sameBool(a, b *bool) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func (d *database) writeWarning(value *bool) error {
	dataKey, existsKey, blobKey := d.key(1, warningKey), d.key(2, warningKey), d.key(3, warningKey)
	if _, ok := d.records[string(blobKey)]; ok {
		return errEncoding
	}
	batch := new(leveldb.Batch)
	expected := maps.Clone(d.records)
	if value == nil {
		batch.Delete(dataKey)
		batch.Delete(existsKey)
		delete(expected, string(dataKey))
		delete(expected, string(existsKey))
	} else {
		versionKey := append(prefix(d.databaseID, 0, 0), 50)
		versionKey = binary.AppendUvarint(versionKey, d.storeID)
		versionKey = append(versionKey, 4)
		last, ok := d.records[string(versionKey)]
		if !ok {
			return errEncoding
		}
		version := littleInt(last)
		if version >= math.MaxInt64 {
			return errEncoding
		}
		version++
		encoded := append(binary.AppendUvarint(nil, version), boolHeader...)
		tag := byte('F')
		if *value {
			tag = 'T'
		}
		encoded = append(encoded, tag)
		for k, v := range map[string][]byte{string(dataKey): encoded, string(existsKey): encodeInt(version), string(versionKey): encodeInt(version)} {
			batch.Put([]byte(k), v)
			expected[k] = v
		}
	}
	if err := d.db.Write(batch, &opt.WriteOptions{Sync: true}); err != nil {
		return err
	}
	after, err := d.snapshot()
	if err != nil {
		return err
	}
	if !maps.EqualFunc(after, expected, bytes.Equal) {
		return fmt.Errorf("警告設定以外の変更を検出しました。実設定には反映しません")
	}
	d.records = after
	return nil
}
