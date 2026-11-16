package service

import (
	"bytes"
	"encoding/json"

	"github.com/addp/common/dataitem/authorizationfacts"
	commonjson "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
)

// These formats have no external constituents. Formats with their own
// composition contract never enter this native single-object projection.
func singleNativeObjectFormat(format string) bool {
	switch format {
	case "csv", "json", "parquet", "text", "png":
		return true
	}
	return false
}

func projectSingleObjectAuthorizationFacts(lookup metadataNodeLookup, item models.MetaItem, facts *authorizationfacts.AuthorizationFacts) error {
	if commonjson.String(item.Attributes, "item", "layout") != "single" || commonjson.String(item.Attributes, "storage", "physical_path") != item.FullName {
		return nil
	}
	target, err := metadataItemTarget(lookup, item)
	if err != nil {
		if err == errMetadataItemIdentity {
			return nil
		}
		return err
	}
	if raw, exists := commonjson.Section(item.Attributes, "item")["refs"]; exists {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		var refs []fileAuthorizationRef
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&refs) != nil || len(refs) != 1 || refs[0].Path != item.FullName || refs[0].Role != "main" || !refs[0].Primary || refs[0].Required == nil || !*refs[0].Required {
			return nil
		}
	}
	facts.Boundary = &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: target}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		return err
	}
	facts.BoundaryHash = &hash
	facts.BasisState = authorizationfacts.AuthorizationBasisComplete
	return facts.Validate()
}
