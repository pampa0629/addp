package skp

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// One TLV framing path for private known-role readers. Offsets passed to the
// callback address payloads; retained opaque offsets address record headers.
type vffRecords struct {
	ctx                           context.Context
	maxRecords, maxDepth, records int
}
type vffOpaqueRecord struct {
	member, location string
	bytes, offset    int
	sha256           [32]byte
}

func vffOpaque(member, location string, payload []byte, offset int) vffOpaqueRecord {
	return vffOpaqueRecord{member: member, location: location, bytes: len(payload), offset: offset, sha256: sha256.Sum256(payload)}
}

func (r *vffRecords) each(raw []byte, depth, base int, consume func(string, []byte, int) error) error {
	if r.maxRecords <= 0 || r.maxDepth <= 0 || depth > r.maxDepth {
		return fmt.Errorf("VFF record depth budget exceeded or unspecified")
	}
	for len(raw) > 0 {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		r.records++
		if r.records > r.maxRecords {
			return fmt.Errorf("VFF record count budget exceeded")
		}
		if len(raw) < 6 {
			return fmt.Errorf("truncated VFF record")
		}
		size := uint64(binary.LittleEndian.Uint32(raw[2:6]))
		if size > uint64(len(raw)-6) {
			return fmt.Errorf("truncated VFF record payload")
		}
		tag := fmt.Sprintf("%02X%02X", raw[0], raw[1])
		if err := consume(tag, raw[6:6+int(size)], base+6); err != nil {
			return err
		}
		base += 6 + int(size)
		raw = raw[6+int(size):]
	}
	return r.ctx.Err()
}

func vffRecordID(raw []byte) (uint64, error) {
	if len(raw) < 1 || len(raw) > 8 {
		return 0, fmt.Errorf("invalid VFF record ID size")
	}
	var value uint64
	for i, b := range raw {
		value |= uint64(b) << (8 * i)
	}
	if value == 0 {
		return 0, fmt.Errorf("zero VFF record ID")
	}
	return value, nil
}
func (r *vffRecords) entityID(raw []byte, depth, base int) (uint64, error) {
	var id uint64
	count := 0
	err := r.each(raw, depth, base, func(tag string, data []byte, offset int) error {
		count++
		if tag != "DE05" || count != 1 {
			return fmt.Errorf("ambiguous VFF record entity ID")
		}
		var err error
		id, err = vffRecordID(data)
		return err
	})
	if err != nil {
		return 0, err
	}
	if count != 1 {
		return 0, fmt.Errorf("missing VFF record entity ID")
	}
	return id, nil
}
