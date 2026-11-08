package authorization

import (
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/addp/common/engine/plugin"
)

// SourceSearchCheckRequest is fixed to Manager search. A null expected tag
// establishes the basis; subsequent batches must supply the original tag.
type SourceSearchCheckRequest struct {
	Targets                []plugin.EngineCatalogPath `json:"targets"`
	QueryNonce             string                     `json:"query_nonce"`
	ExpectedObservationTag *string                    `json:"expected_observation_tag"`
}

func (r SourceSearchCheckRequest) Valid() bool {
	nonce, err := hex.DecodeString(r.QueryNonce)
	if err != nil || len(nonce) != 16 || r.QueryNonce != strings.ToLower(r.QueryNonce) || len(r.Targets) > 200 {
		return false
	}
	if len(r.Targets) > 0 && !(SourceMetadataCheckRequest{Targets: r.Targets}).Valid() {
		return false
	}
	return r.ExpectedObservationTag == nil || ValidSearchObservationTag(*r.ExpectedObservationTag)
}

func ValidSearchObservationTag(tag string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(tag)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == tag
}
