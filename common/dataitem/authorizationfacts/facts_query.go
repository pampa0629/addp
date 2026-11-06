package authorizationfacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

const MaxAuthorizationFactsQueryItems = 64
const MaxAuthorizationFactsQueryBytes = 8 << 10
const MaxAuthorizationFactsBatchBytes = MaxAuthorizationFactsQueryItems*(MaxAuthorizationFactsBytes+256) + 256

// AuthorizationFactsQuery is an exact, bounded owner read, never a permission.
type AuthorizationFactsQuery struct {
	Fingerprints []string `json:"fingerprints"`
}

func (q AuthorizationFactsQuery) Validate() error {
	if len(q.Fingerprints) == 0 || len(q.Fingerprints) > MaxAuthorizationFactsQueryItems {
		return errors.New("invalid authorization facts query size")
	}
	seen := map[string]bool{}
	for _, fp := range q.Fingerprints {
		if !ValidItemFingerprint(fp) || seen[fp] {
			return errors.New("invalid authorization facts query identity")
		}
		seen[fp] = true
	}
	return nil
}

// Facts is explicitly null for an absent/deleted item in the current tenant.
// A failed dependency is an error for the whole query, never a null entry.
type AuthorizationFactsResult struct {
	ItemFingerprint string              `json:"item_fingerprint"`
	Facts           *AuthorizationFacts `json:"facts"`
}

type AuthorizationFactsBatch struct {
	Data []AuthorizationFactsResult `json:"data"`
}

func (b *AuthorizationFactsBatch) Validate(q AuthorizationFactsQuery) error {
	if b == nil || q.Validate() != nil || len(b.Data) != len(q.Fingerprints) {
		return errors.New("incomplete authorization facts batch")
	}
	for i, result := range b.Data {
		if result.ItemFingerprint != q.Fingerprints[i] || result.Facts != nil && (result.Facts.ItemFingerprint != result.ItemFingerprint || result.Facts.Validate() != nil) {
			return errors.New("unbound authorization facts result")
		}
		if result.Facts != nil {
			encoded, err := json.Marshal(result.Facts)
			if err != nil || len(encoded) > MaxAuthorizationFactsBytes {
				return errors.New("authorization fact exceeds wire budget")
			}
		}
	}
	return nil
}

func decodeFactsObject(raw []byte, maxBytes int, value any, required ...string) error {
	if len(raw) > maxBytes || !utf8.Valid(raw) {
		return errors.New("invalid authorization facts envelope size")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	keys, err := authorizationJSONValue(d, 0, 24)
	if err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing authorization facts envelope")
	}
	for _, key := range required {
		if !keys[key] {
			return errors.New("missing authorization facts envelope field")
		}
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(value)
}

func (r *AuthorizationFactsResult) UnmarshalJSON(raw []byte) error {
	type wire AuthorizationFactsResult
	var value wire
	if err := decodeFactsObject(raw, MaxAuthorizationFactsBytes+256, &value, "item_fingerprint", "facts"); err != nil {
		return err
	}
	if !ValidItemFingerprint(value.ItemFingerprint) {
		return errors.New("invalid authorization facts result identity")
	}
	*r = AuthorizationFactsResult(value)
	return nil
}

func (b *AuthorizationFactsBatch) UnmarshalJSON(raw []byte) error {
	type wire AuthorizationFactsBatch
	var value wire
	if err := decodeFactsObject(raw, MaxAuthorizationFactsBatchBytes, &value, "data"); err != nil {
		return err
	}
	if len(value.Data) == 0 || len(value.Data) > MaxAuthorizationFactsQueryItems {
		return errors.New("invalid authorization facts result count")
	}
	*b = AuthorizationFactsBatch(value)
	return nil
}
