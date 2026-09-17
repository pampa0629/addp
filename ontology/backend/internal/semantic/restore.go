package semantic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func (s *Snapshot) Scope() Scope { return s.definition.Scope }

// Restore accepts only the exact canonical package produced by the current
// contract/compiler. It never upgrades or silently normalizes stored history.
func Restore(data []byte, digest string) (*Snapshot, error) {
	if len(data) > 2<<20 {
		return nil, fmt.Errorf("snapshot_size_limit")
	}
	var envelope struct {
		Contract   string     `json:"contract"`
		Compiler   string     `json:"compiler"`
		Definition Definition `json:"definition"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&envelope); err != nil {
		return nil, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("invalid_snapshot_trailer")
	}
	if envelope.Contract != ContractVersion || envelope.Compiler != CompilerVersion {
		return nil, fmt.Errorf("unsupported_snapshot_version")
	}
	s, err := Freeze(envelope.Definition)
	if err != nil {
		return nil, err
	}
	if s.Digest() != digest || !bytes.Equal(data, s.canonical) {
		return nil, fmt.Errorf("snapshot_integrity_mismatch")
	}
	return s, nil
}
