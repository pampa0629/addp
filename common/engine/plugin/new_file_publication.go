package plugin

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"

	"github.com/google/uuid"
)

// NewFilePublicationProvider publishes complete bytes without replacing a leaf.
// The trusted execution owner persists preparation and authorization before Publish.
// Neither a preparation nor a publication outcome is an IAM permission.
type NewFilePublicationProvider interface {
	EnginePlugin
	PrepareNewFile(context.Context, ConnectionInfo, NewFilePublicationRequest, io.Reader) (PreparedNewFile, error)
	// The final argument is the owner's persisted state BEFORE claiming dispatch.
	// Only not_published can claim a new dispatch; unknown is resolve-only.
	PublishNewFile(context.Context, ConnectionInfo, PreparedNewFile, NewFilePublicationState) (NewFilePublicationOutcome, error)
	ResolveNewFile(context.Context, ConnectionInfo, PreparedNewFile) (NewFilePublicationOutcome, error)
	CleanupNewFile(context.Context, ConnectionInfo, PreparedNewFile, NewFilePublicationState) error
}

type NewFilePublicationRequest struct {
	OperationID string            `json:"operation_id"`
	Target      EngineCatalogPath `json:"target"`
	SizeBytes   int64             `json:"size_bytes"`
	SHA256      string            `json:"sha256"`
}

func (r NewFilePublicationRequest) Validate() error {
	id, err := uuid.Parse(r.OperationID)
	if err != nil || id == uuid.Nil || id.String() != r.OperationID {
		return errors.New("invalid publication operation identity")
	}
	if _, err := RequireFileLeafPath(r.Target); err != nil {
		return err
	}
	if r.Target.EngineID == 0 || !reflect.DeepEqual(r.Target, NormalizeFileCatalogSegments(r.Target)) {
		return errors.New("publication requires a canonical engine file leaf")
	}
	for i, segment := range r.Target.Segments {
		if i == 0 {
			if segment.Kind != EngineCatalogKindRoot || segment.Term != EngineCatalogTermRoot {
				return errors.New("publication requires explicit file root")
			}
			continue
		}
		if segment.Name == "" || segment.Name == "." || segment.Name == ".." || strings.ContainsAny(segment.Name, "/\x00") {
			return errors.New("invalid publication path segment")
		}
		term, kind := EngineCatalogTermDirectory, EngineCatalogKindDirectory
		if i == len(r.Target.Segments)-1 {
			term, kind = EngineCatalogTermFile, EngineCatalogKindFile
		}
		if segment.Term != term || segment.Kind != kind {
			return errors.New("invalid publication file path shape")
		}
	}
	digest, err := hex.DecodeString(r.SHA256)
	if r.SizeBytes < 0 || r.SizeBytes == math.MaxInt64 || err != nil || len(digest) != 32 || strings.ToLower(r.SHA256) != r.SHA256 {
		return errors.New("invalid prepared content size or digest")
	}
	return nil
}

// ProviderState is private recovery data: the owner must persist it separately
// and never serialize it into public execution outputs, diagnostics or browsers.
type PreparedNewFile struct {
	NewFilePublicationRequest
	ProviderState []byte `json:"-"`
}

// NewFilePreparationError means private side effects may remain, even when
// Prepare returned no confirmed file identity. Persist the original operation
// and the separately returned private state; never infer cleanup from error.
type NewFilePreparationError struct {
	OperationID    string
	CleanupPending bool
	Cause          error
}

func (e *NewFilePreparationError) Error() string {
	return "file preparation requires private reconciliation"
}
func (e *NewFilePreparationError) Unwrap() error { return e.Cause }

type NewFilePublicationState string

const (
	NewFileNotPublished NewFilePublicationState = "not_published"
	NewFilePublished    NewFilePublicationState = "published"
	NewFileUnknown      NewFilePublicationState = "unknown"
)

type NewFilePublicationOutcome struct {
	State          NewFilePublicationState
	Evidence       *ContentWriteEvidence
	CleanupPending bool
}

func (o NewFilePublicationOutcome) Validate(p PreparedNewFile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if o.State == NewFilePublished {
		if o.Evidence == nil || o.Evidence.Validate(p.Target) != nil || o.Evidence.Disposition != "created" || o.Evidence.SizeBytes != p.SizeBytes || !reflect.DeepEqual(o.Evidence.Target, p.Target) {
			return errors.New("publication evidence does not match prepared bytes")
		}
	} else if (o.State != NewFileNotPublished && o.State != NewFileUnknown) || o.Evidence != nil {
		return errors.New("invalid publication outcome")
	}
	return nil
}
