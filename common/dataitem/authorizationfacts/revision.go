package authorizationfacts

import (
	"errors"
	"strconv"

	"github.com/google/uuid"
)

const RevisionSchemaVersion = "addp.authorization_basis_revision/v1"

// Revision identifies committed owner facts. It conveys no resource identity or Allow.
type Revision struct {
	SchemaVersion string `json:"schema_version"`
	Generation    string `json:"generation"`
	Version       string `json:"version"`
}

func (r Revision) Validate() error {
	version, err := strconv.ParseUint(r.Version, 10, 64)
	id, idErr := uuid.Parse(r.Generation)
	if r.SchemaVersion != RevisionSchemaVersion || err != nil || strconv.FormatUint(version, 10) != r.Version || idErr != nil || id == uuid.Nil || id.String() != r.Generation {
		return errors.New("invalid authorization basis revision")
	}
	return nil
}
