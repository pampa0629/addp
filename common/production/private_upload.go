package production

import (
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// PrivateUploadProof records the private upload consumed by the same
// Transfer attempt. It is producer evidence, never a DataItem or Grant.
type PrivateUploadProof struct {
	UploadID       string `json:"upload_id"`
	UploaderID     int64  `json:"uploader_id,string" swaggertype:"string"`
	TaskID         uint   `json:"task_id,string" swaggertype:"string"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

func (p PrivateUploadProof) Validate() error {
	id, err := uuid.Parse(p.UploadID)
	digest, hashErr := hex.DecodeString(p.ManifestSHA256)
	if err != nil || id == uuid.Nil || id.String() != p.UploadID || p.UploaderID <= 0 || p.TaskID == 0 ||
		hashErr != nil || len(digest) != 32 || strings.ToLower(p.ManifestSHA256) != p.ManifestSHA256 {
		return errors.New("invalid Manager import read proof")
	}
	return nil
}
