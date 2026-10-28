package authorizationfacts

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const MaxIdentityQueryFingerprints = 20000
const MaxIdentityQueryBytes = 2 << 20

// ErrIdentityQueryCapacity distinguishes an otherwise bounded query whose
// complete identity predicate cannot fit from malformed client input.
var ErrIdentityQueryCapacity = errors.New("DataItem identity query exceeds fingerprint capacity")

// IdentityQuery is a value-only owner query. It carries no permission or ACL.
type IdentityQuery struct {
	EngineID          uint     `json:"engine_id"`
	Search            string   `json:"search,omitempty"`
	FingerprintFilter string   `json:"fingerprint_filter,omitempty" enums:"include,exclude"`
	Fingerprints      []string `json:"fingerprints,omitempty"`
	Page              int      `json:"page"`
	PageSize          int      `json:"page_size"`
}

func (q IdentityQuery) Validate() error {
	if q.EngineID == 0 || uint64(q.EngineID) > 1<<32-1 || q.Page < 1 || q.Page > 1<<31-1 || q.PageSize < 1 || q.PageSize > 100 ||
		len(q.Search) > 512 || !utf8.ValidString(q.Search) || strings.ContainsRune(q.Search, 0) ||
		(q.FingerprintFilter != "" && q.FingerprintFilter != "include" && q.FingerprintFilter != "exclude") ||
		(q.FingerprintFilter == "" && len(q.Fingerprints) != 0) {
		return errors.New("invalid DataItem identity query")
	}
	if len(q.Fingerprints) > MaxIdentityQueryFingerprints {
		return ErrIdentityQueryCapacity
	}
	seen := make(map[string]bool, len(q.Fingerprints))
	for _, value := range q.Fingerprints {
		if !ValidItemFingerprint(value) || seen[value] {
			return errors.New("invalid DataItem identity filter")
		}
		seen[value] = true
	}
	return nil
}

type DataItemIdentity struct {
	ItemFingerprint string `json:"item_fingerprint"`
	EngineID        uint   `json:"engine_id"`
	Name            string `json:"name"`
	FullName        string `json:"full_name"`
	ItemType        string `json:"item_type"`
}

type IdentityPage struct {
	Data       []DataItemIdentity `json:"data"`
	Total      int64              `json:"total"`
	Page       int                `json:"page"`
	PageSize   int                `json:"page_size"`
	TotalPages int64              `json:"total_pages"`
}

func (p IdentityPage) Validate(q IdentityQuery) error {
	if q.Validate() != nil || p.Data == nil || p.Total < 0 || p.Page != q.Page || p.PageSize != q.PageSize ||
		p.TotalPages != p.Total/int64(q.PageSize)+boolInt(p.Total%int64(q.PageSize) != 0) {
		return errors.New("invalid DataItem identity page")
	}
	want := p.Total - int64(q.Page-1)*int64(q.PageSize)
	if want < 0 {
		want = 0
	}
	if want > int64(q.PageSize) {
		want = int64(q.PageSize)
	}
	if int64(len(p.Data)) != want {
		return errors.New("incomplete DataItem identity page")
	}
	set := make(map[string]bool, len(q.Fingerprints))
	for _, key := range q.Fingerprints {
		set[key] = true
	}
	previous := ""
	for _, item := range p.Data {
		if !ValidItemFingerprint(item.ItemFingerprint) || item.ItemFingerprint <= previous || item.EngineID != q.EngineID ||
			item.Name == "" || item.FullName == "" || item.ItemType == "" ||
			(q.FingerprintFilter == "include" && !set[item.ItemFingerprint]) || (q.FingerprintFilter == "exclude" && set[item.ItemFingerprint]) {
			return errors.New("invalid DataItem identity")
		}
		previous = item.ItemFingerprint
	}
	return nil
}

func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
