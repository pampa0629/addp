package execution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"

	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
)

// ManagerProfileReadScope is a wire boundary, never an access credential. IAM
// owns issuance; Manager owns the matching immutable Provider plan.
type ManagerProfileReadScope struct {
	ConfigDigest string              `json:"config_digest"`
	ReadSet      plugin.QueryReadSet `json:"read_set"`
}

// NewManagerProfileReadScope binds the entire persisted JSON configuration to
// the actual Provider read set. It does not interpret the owner's private
// schema or authorize anything. Callers must supply raw database JSON, not a
// JSONMap that has already passed through float64.
func NewManagerProfileReadScope(raw json.RawMessage, readSet *plugin.QueryReadSet) (*ManagerProfileReadScope, error) {
	if len(raw) == 0 || len(raw) > 512<<10 || readSet == nil {
		return nil, errors.New("invalid Manager profile configuration")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var config map[string]any
	if err := decoder.Decode(&config); err != nil || config == nil {
		return nil, errors.New("invalid Manager profile configuration")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid Manager profile configuration")
	}
	canonical, err := json.Marshal(config)
	if err != nil {
		return nil, errors.New("invalid Manager profile configuration")
	}
	digest := sha256.Sum256(canonical)
	scope := &ManagerProfileReadScope{ConfigDigest: hex.EncodeToString(digest[:]), ReadSet: *readSet.Clone()}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return scope, nil
}

func (s *ManagerProfileReadScope) Validate() error {
	invalid := errors.New("invalid complete Manager profile source scope")
	if s == nil || len(s.ConfigDigest) != 64 || len(s.ReadSet.Paths) == 0 || len(s.ReadSet.Paths) > 200 {
		return invalid
	}
	for _, char := range s.ConfigDigest {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return invalid
		}
	}
	canonical, err := plugin.NewQueryReadSet(s.ReadSet.Paths...)
	if err != nil || !reflect.DeepEqual(canonical, &s.ReadSet) {
		return invalid
	}
	for _, path := range s.ReadSet.Paths {
		if path.EngineID > uint(1<<63-1) || path.EngineID != s.ReadSet.Paths[0].EngineID {
			return invalid
		}
		if _, err := authorization.EncodeSharingTarget(path); err != nil {
			return invalid
		}
	}
	return nil
}

func (s *ManagerProfileReadScope) Clone() *ManagerProfileReadScope {
	if s == nil {
		return nil
	}
	return &ManagerProfileReadScope{ConfigDigest: s.ConfigDigest, ReadSet: *s.ReadSet.Clone()}
}
