// Copyright 2013 The Chromium Authors.
// Copyright 2026 NVIDIAReplayMonitor contributors.
// Ordering follows Chromium 128's indexed_db_leveldb_coding.cc (BSD-3-Clause).
package notification

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"math"
	"sync/atomic"
	"unicode/utf16"
)

var errEncoding = errors.New("未対応または不正なIndexedDB形式です")

type decoder struct{ data []byte }

func (d *decoder) take(n int) []byte {
	if n < 0 || n > len(d.data) {
		panic(errEncoding)
	}
	b := d.data[:n]
	d.data = d.data[n:]
	return b
}
func (d *decoder) byte() byte { return d.take(1)[0] }
func (d *decoder) variable() uint64 {
	v, n := binary.Uvarint(d.data)
	if n <= 0 || v > math.MaxInt64 {
		panic(errEncoding)
	}
	d.take(n)
	return v
}
func (d *decoder) text() []byte {
	n := d.variable()
	if n > uint64(len(d.data)/2) {
		panic(errEncoding)
	}
	return d.take(int(n) * 2)
}
func (d *decoder) binary() []byte {
	n := d.variable()
	if n > uint64(len(d.data)) {
		panic(errEncoding)
	}
	return d.take(int(n))
}
func littleInt(b []byte) uint64 {
	if len(b) == 0 || len(b) > 8 {
		panic(errEncoding)
	}
	var v uint64
	for i, c := range b {
		v |= uint64(c) << uint(i*8)
	}
	return v
}
func encodeInt(v uint64) []byte {
	b := []byte{byte(v)}
	for v >>= 8; v > 0; v >>= 8 {
		b = append(b, byte(v))
	}
	return b
}
func prefix(db, store, index uint64) []byte {
	a, b, c := encodeInt(db), encodeInt(store), encodeInt(index)
	out := []byte{byte((len(a)-1)<<5 | (len(b)-1)<<2 | (len(c) - 1))}
	out = append(out, a...)
	out = append(out, b...)
	return append(out, c...)
}
func readPrefix(d *decoder) [3]uint64 {
	b := d.byte()
	return [3]uint64{littleInt(d.take(int(b>>5) + 1)), littleInt(d.take(int((b>>2)&7) + 1)), littleInt(d.take(int(b&3) + 1))}
}
func encodeText(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := binary.AppendUvarint(nil, uint64(len(units)))
	for _, v := range units {
		out = append(out, byte(v>>8), byte(v))
	}
	return out
}
func decodeText(b []byte) string {
	if len(b)%2 != 0 {
		panic(errEncoding)
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(b[i*2:])
	}
	return string(utf16.Decode(units))
}

type keyPart struct {
	number uint64
	text   []byte
	isText bool
}
type decodedKey struct {
	prefix [3]uint64
	parts  []keyPart
	suffix []byte
}

func decodeKey(b []byte) (key decodedKey, err error) {
	defer func() {
		if recover() != nil {
			err = errEncoding
		}
	}()
	d := decoder{b}
	key.prefix = readPrefix(&d)
	if key.prefix[0] == 0 && (key.prefix[1] != 0 || key.prefix[2] != 0) || key.prefix[1] == 0 && key.prefix[2] != 0 {
		return key, errEncoding
	}
	if key.prefix[1] != 0 {
		key.suffix = d.data
		validateDataKey(key.prefix[2], d)
		return key, nil
	}
	number := func(v uint64) { key.parts = append(key.parts, keyPart{number: v}) }
	text := func(b []byte) { key.parts = append(key.parts, keyPart{text: b, isText: true}) }
	kind := d.byte()
	number(uint64(kind))
	if key.prefix[0] == 0 {
		switch {
		case kind < 7:
		case kind == 50:
			text(d.take(len(d.data)))
		case kind == 100:
			number(d.variable())
		case kind == 201:
			text(d.text())
			text(d.text())
		default:
			return key, errEncoding
		}
	} else {
		switch {
		case kind < 6:
		case kind == 50:
			number(d.variable())
			number(uint64(d.byte()))
		case kind == 100:
			number(d.variable())
			number(d.variable())
			number(uint64(d.byte()))
		case kind == 150:
			number(d.variable())
		case kind == 151:
			number(d.variable())
			number(d.variable())
		case kind == 200:
			text(d.text())
		case kind == 201:
			number(d.variable())
			text(d.text())
		default:
			return key, errEncoding
		}
	}
	if len(d.data) != 0 {
		return key, errEncoding
	}
	return key, nil
}

func validateDataKey(index uint64, d decoder) {
	if index == 0 || (index > 3 && index < 30) {
		panic(errEncoding)
	}
	if len(d.data) == 0 {
		return
	} // LevelDB seek boundary.
	consumeIDBKey(&d, 0)
	if index >= 30 && len(d.data) > 0 {
		d.variable()
		if len(d.data) > 0 {
			consumeIDBKey(&d, 0)
		}
	}
	if len(d.data) != 0 {
		panic(errEncoding)
	}
}
func consumeIDBKey(d *decoder, depth int) {
	if depth > 64 {
		panic(errEncoding)
	}
	switch d.byte() {
	case 0, 5:
	case 1:
		d.text()
	case 2, 3:
		if math.IsNaN(math.Float64frombits(binary.LittleEndian.Uint64(d.take(8)))) {
			panic(errEncoding)
		}
	case 4:
		n := d.variable()
		if n > uint64(len(d.data)) {
			panic(errEncoding)
		}
		for ; n > 0; n-- {
			consumeIDBKey(d, depth+1)
		}
	case 6:
		d.binary()
	default:
		panic(errEncoding)
	}
}

type idbComparer struct{ invalid atomic.Bool }

func (*idbComparer) Name() string                    { return "idb_cmp1" }
func (*idbComparer) Separator(_, _, _ []byte) []byte { return nil }
func (*idbComparer) Successor(_, _ []byte) []byte    { return nil }
func (c *idbComparer) Compare(a, b []byte) int {
	if len(a) == 0 || len(b) == 0 {
		return cmp.Compare(len(a), len(b))
	}
	ka, ea := decodeKey(a)
	kb, eb := decodeKey(b)
	if ea != nil || eb != nil {
		c.invalid.Store(true)
		return bytes.Compare(a, b)
	}
	for i := range ka.prefix {
		if n := cmp.Compare(ka.prefix[i], kb.prefix[i]); n != 0 {
			return n
		}
	}
	if ka.prefix[1] != 0 {
		return compareData(ka.prefix[2], ka.suffix, kb.suffix)
	}
	for i := 0; i < len(ka.parts) && i < len(kb.parts); i++ {
		x, y := ka.parts[i], kb.parts[i]
		var n int
		if x.isText && y.isText {
			n = bytes.Compare(x.text, y.text)
		} else {
			n = cmp.Compare(x.number, y.number)
		}
		if n != 0 {
			return n
		}
	}
	return cmp.Compare(len(ka.parts), len(kb.parts))
}
func compareData(index uint64, a, b []byte) int {
	if len(a) == 0 || len(b) == 0 {
		return cmp.Compare(len(a), len(b))
	}
	da, db := decoder{a}, decoder{b}
	if n := compareIDBKey(&da, &db); n != 0 {
		return n
	}
	if index < 30 {
		return 0
	}
	var sa, sb uint64
	if len(da.data) > 0 {
		sa = da.variable()
	}
	if len(db.data) > 0 {
		sb = db.variable()
	}
	if len(da.data) == 0 || len(db.data) == 0 {
		return cmp.Compare(len(da.data), len(db.data))
	}
	if n := compareIDBKey(&da, &db); n != 0 {
		return n
	}
	return cmp.Compare(sa, sb)
}
func compareIDBKey(a, b *decoder) int {
	ta, tb := a.byte(), b.byte()
	rank := [7]int{6, 3, 2, 1, 5, 0, 4}
	if n := cmp.Compare(rank[ta], rank[tb]); n != 0 {
		return n
	}
	switch ta {
	case 1:
		return bytes.Compare(a.text(), b.text())
	case 6:
		return bytes.Compare(a.binary(), b.binary())
	case 2, 3:
		return cmp.Compare(math.Float64frombits(binary.LittleEndian.Uint64(a.take(8))), math.Float64frombits(binary.LittleEndian.Uint64(b.take(8))))
	case 4:
		na, nb := a.variable(), b.variable()
		for i := uint64(0); i < na && i < nb; i++ {
			if n := compareIDBKey(a, b); n != 0 {
				return n
			}
		}
		return cmp.Compare(na, nb)
	}
	return 0
}
