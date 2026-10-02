package authorization

import (
	"encoding/json"
	"errors"
	"math"
	"unicode/utf8"

	"github.com/addp/common/engine/plugin"
)

var ErrInvalidSharingTarget = errors.New("invalid ordinary read-sharing target")

// EncodeSharingTarget is the single value boundary for Catalog preparation
// and System arbitration. It preserves exact structural identity; it does not
// authenticate, authorize, discover the source or change the engine path model.
func EncodeSharingTarget(path plugin.EngineCatalogPath) (json.RawMessage, error) {
	if path.EngineID == 0 || uint64(path.EngineID) > math.MaxInt64 || len(path.Segments) > 64 {
		return nil, ErrInvalidSharingTarget
	}
	if _, err := plugin.NewQueryReadSet(path); err != nil {
		return nil, ErrInvalidSharingTarget
	}
	for _, segment := range path.Segments {
		if !utf8.ValidString(segment.Term) || !utf8.ValidString(segment.Kind) || !utf8.ValidString(segment.Name) {
			return nil, ErrInvalidSharingTarget
		}
	}
	encoded, err := json.Marshal(path)
	if err != nil || len(encoded) > 16*1024 {
		return nil, ErrInvalidSharingTarget
	}
	return encoded, nil
}
