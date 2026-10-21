package auditarchive

import (
	"bytes"
	"encoding/binary"
	"io"
	"time"
)

type relation struct {
	ID      uint32
	Columns []string
}
type decoder struct {
	relation      relation
	inTransaction bool
	rows          []Row
	bytes         int
}
type commit struct {
	EndLSN uint64
	At     time.Time
	Rows   []Row
}

// PostgreSQL pgoutput protocol v1; binary and streamed transactions are not
// requested. Unexpected DDL/relations or mutation messages stop consumption.
func (d *decoder) parse(data []byte) (*commit, error) {
	if len(data) < 1 {
		return nil, ErrUnknown
	}
	r := bytes.NewReader(data[1:])
	read := func(v any) error { return binary.Read(r, binary.BigEndian, v) }
	switch data[0] {
	case 'B':
		if len(data) != 21 || d.inTransaction {
			return nil, ErrUnknown
		}
		d.inTransaction = true
		d.rows = nil
		d.bytes = 0
	case 'R':
		var id uint32
		if read(&id) != nil {
			return nil, ErrUnknown
		}
		schema, e := cstring(r)
		if e != nil {
			return nil, e
		}
		table, e := cstring(r)
		if e != nil {
			return nil, e
		}
		if schema != "system" || table != "audit_logs" {
			return nil, ErrUnknown
		}
		if _, e = r.ReadByte(); e != nil {
			return nil, e
		}
		var count uint16
		if read(&count) != nil || count > 128 {
			return nil, ErrUnknown
		}
		columns := make([]string, 0, count)
		for i := 0; i < int(count); i++ {
			if _, e = r.ReadByte(); e != nil {
				return nil, e
			}
			name, e := cstring(r)
			if e != nil {
				return nil, e
			}
			var oid, modifier uint32
			if read(&oid) != nil || read(&modifier) != nil {
				return nil, ErrUnknown
			}
			for _, c := range columns {
				if c == name {
					return nil, ErrUnknown
				}
			}
			columns = append(columns, name)
		}
		if r.Len() != 0 {
			return nil, ErrUnknown
		}
		// A schema change requires an explicit protocol/spec revision, not
		// silent acceptance of a new field which could contain sensitive data.
		expected := auditColumns
		if len(columns) != len(expected) {
			return nil, ErrUnknown
		}
		for i, c := range columns {
			if c != expected[i] {
				return nil, ErrUnknown
			}
		}
		d.relation = relation{ID: id, Columns: columns}
	case 'I':
		if !d.inTransaction {
			return nil, ErrUnknown
		}
		var id uint32
		if read(&id) != nil || id != d.relation.ID || len(d.relation.Columns) == 0 {
			return nil, ErrUnknown
		}
		kind, e := r.ReadByte()
		if e != nil || kind != 'N' {
			return nil, ErrUnknown
		}
		var count uint16
		if read(&count) != nil || int(count) != len(d.relation.Columns) {
			return nil, ErrUnknown
		}
		row := Row{}
		d.bytes += len(d.relation.Columns) * 32
		if d.bytes > MaxObjectBytes/2 {
			return nil, ErrBudget
		}
		for _, name := range d.relation.Columns {
			kind, e = r.ReadByte()
			if e != nil {
				return nil, e
			}
			if kind == 'n' {
				row[name] = nil
				continue
			}
			if kind != 't' {
				return nil, ErrUnknown
			}
			var size uint32
			if read(&size) != nil || size > MaxObjectBytes || uint64(size) > uint64(r.Len()) {
				return nil, ErrBudget
			}
			d.bytes += int(size)
			if d.bytes > MaxObjectBytes/2 {
				return nil, ErrBudget
			}
			b := make([]byte, size)
			if _, e = io.ReadFull(r, b); e != nil {
				return nil, e
			}
			s := string(b)
			row[name] = &s
		}
		if r.Len() != 0 || !validRow(row) {
			return nil, ErrUnknown
		}
		d.rows = append(d.rows, row)
	case 'C':
		if !d.inTransaction || len(data) != 26 {
			return nil, ErrUnknown
		}
		flags, e := r.ReadByte()
		if e != nil || flags != 0 {
			return nil, ErrUnknown
		}
		var lsn, end uint64
		var micros int64
		if read(&lsn) != nil || read(&end) != nil || read(&micros) != nil || end < lsn {
			return nil, ErrUnknown
		}
		c := &commit{EndLSN: end, At: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(micros) * time.Microsecond), Rows: d.rows}
		d.inTransaction = false
		d.rows = nil
		return c, nil
	default:
		return nil, ErrUnknown
	}
	return nil, nil
}

func cstring(r *bytes.Reader) (string, error) {
	var b []byte
	for r.Len() > 0 {
		c, e := r.ReadByte()
		if e != nil {
			return "", e
		}
		if c == 0 {
			return string(b), nil
		}
		b = append(b, c)
		if len(b) > 256 {
			return "", ErrBudget
		}
	}
	return "", ErrUnknown
}
