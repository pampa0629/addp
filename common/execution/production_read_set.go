package execution

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

// ValidateProductionReadSet is the bounded pure-value contract for the first
// PG production slice. Nil means unavailable, never unbounded permission or a
// proven source-free generation. The owner may not normalize or fill old proof.
func ValidateProductionReadSet(set *plugin.QueryReadSet) error {
	if set == nil {
		return nil
	}
	if len(set.Paths) == 0 || len(set.Paths) > 200 {
		return errors.New("invalid production source read set")
	}
	normalized, err := plugin.NewQueryReadSet(set.Paths...)
	if err != nil || !reflect.DeepEqual(normalized, set) {
		return errors.New("noncanonical production source read set")
	}
	for _, path := range set.Paths {
		if len(path.Segments) != 3 || int64(path.EngineID) <= 0 || !reflect.DeepEqual(path, plugin.TabularItemPath(path.EngineID, plugin.EngineCatalogTermSchema, path.Segments[1].Name, path.Segments[2].Name)) {
			return errors.New("unsupported production source read set")
		}
	}
	if encoded, err := json.Marshal(set); err != nil || len(encoded) > 128<<10 {
		return errors.New("production source read set exceeds contract size")
	}
	return nil
}

// ManagerImportReadProof records the private upload consumed by the same
// Transfer attempt. It is producer evidence, never a DataItem or Grant.
type ManagerImportReadProof struct {
	UploadID       string `json:"upload_id"`
	UploaderID     int64  `json:"uploader_id,string" swaggertype:"string"`
	TaskID         uint   `json:"task_id,string" swaggertype:"string"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

func (p ManagerImportReadProof) Validate() error {
	id, err := uuid.Parse(p.UploadID)
	digest, hashErr := hex.DecodeString(p.ManifestSHA256)
	if err != nil || id == uuid.Nil || id.String() != p.UploadID || p.UploaderID <= 0 || p.TaskID == 0 ||
		hashErr != nil || len(digest) != 32 || strings.ToLower(p.ManifestSHA256) != p.ManifestSHA256 {
		return errors.New("invalid Manager import read proof")
	}
	return nil
}
