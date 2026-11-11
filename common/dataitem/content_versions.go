package dataitem

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/common/format"
	commonjson "github.com/addp/common/jsonmap"
)

// ContentVersion is a scanned storage fact, independent of authorization.
type ContentVersion struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

// TIFFContentVersionsFromAttributes binds versions to the exact persisted item
// composition. It does not discover files or decide whether they may be read.
func TIFFContentVersionsFromAttributes(attrs map[string]interface{}) ([]ContentVersion, error) {
	descriptor := DescriptorFromAttributes(attrs)
	if descriptor.Format != "tiff" || descriptor.PhysicalPath == "" ||
		(descriptor.Layout != format.LayoutSingle && descriptor.Layout != format.LayoutMulti) {
		return nil, fmt.Errorf("TIFF content versions require a scanned file item")
	}
	paths := map[string]bool{descriptor.PhysicalPath: true}
	for _, ref := range descriptor.Refs {
		paths[ref.Path] = true
	}
	raw, err := json.Marshal(commonjson.Section(attrs, "storage")["content_versions"])
	if err != nil {
		return nil, err
	}
	var versions []ContentVersion
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&versions) != nil || len(versions) != len(paths) {
		return nil, fmt.Errorf("TIFF content versions do not cover the scanned composition")
	}
	previous := ""
	for _, version := range versions {
		digest, err := hex.DecodeString(version.SHA256)
		if !paths[version.Path] || version.Path <= previous || version.SizeBytes < 0 ||
			err != nil || len(digest) != sha256.Size || version.SHA256 != strings.ToLower(version.SHA256) {
			return nil, fmt.Errorf("invalid TIFF component content version")
		}
		previous = version.Path
	}
	return versions, nil
}
