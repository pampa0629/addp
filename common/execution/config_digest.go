package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/addp/common/jsonmap"
)

const MaxExecutionConfigBytes = 512 << 10

var errExecutionConfig = errors.New("invalid complete execution configuration JSON")

// executionConfigObject is the raw JSON digest boundary for fixed owner
// scopes. It preserves numbers and rejects ambiguous duplicate keys.
func executionConfigObject(raw json.RawMessage) (map[string]any, string, error) {
	object, err := jsonmap.DecodeObjectStrict(raw, MaxExecutionConfigBytes, 64)
	if err != nil {
		return nil, "", errExecutionConfig
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, "", errExecutionConfig
	}
	sum := sha256.Sum256(canonical)
	return object, hex.EncodeToString(sum[:]), nil
}
