package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
)

const (
	SDEVersioningModelTraditional = "traditional"
	SDEVersioningModelBranch      = "branch"

	SDELogicalBootstrapInitial  = "initial"
	SDELogicalPositionType      = "arcgis_sde_logical_position"
	SDELogicalPositionVersionV1 = "v1"
)

var ErrSDELogicalPositionExpired = errors.New("ArcGIS SDE logical position is no longer recoverable")

// SDELogicalSourceDescriptor freezes the registered business table facts used
// throughout one logical-change generation. Registry and geometry field names
// are discovered by the provider and must not be assumed by callers.
type SDELogicalSourceDescriptor struct {
	RepositoryOwner string                `json:"repository_owner"`
	RegistrationID  string                `json:"registration_id"`
	VersioningModel string                `json:"versioning_model"`
	VersionName     string                `json:"version_name"`
	Fields          []datatype.FieldInfo  `json:"fields"`
	Keys            []string              `json:"keys"`
	SpatialInfo     *datatype.SpatialInfo `json:"spatial_info,omitempty"`
}

type SDELogicalChangeOpenOptions struct {
	VersionName    string                `json:"version_name"`
	BootstrapMode  string                `json:"bootstrap_mode"`
	ResumePosition *ChangeStreamPosition `json:"resume_position,omitempty"`
}

// SDELogicalChange is a provider-normalized business-row change. Geometry
// values in Row use EWKB. Delete carries only the stable key values.
type SDELogicalChange struct {
	Operation string                 `json:"operation"`
	Position  ChangeStreamPosition   `json:"position"`
	Key       map[string]interface{} `json:"key"`
	Row       map[string]interface{} `json:"row,omitempty"`
	Snapshot  bool                   `json:"snapshot,omitempty"`
}

type SDELogicalChangeBatch struct {
	Changes           []SDELogicalChange   `json:"changes"`
	EndPosition       ChangeStreamPosition `json:"end_position"`
	BootstrapComplete bool                 `json:"bootstrap_complete,omitempty"`
}

// SDELogicalChangeSourceProvider is a workspace-scoped domain adapter, not an
// Oracle Store capability. The first implementation supports traditional
// versioning only and feeds Transfer's existing Kafka continuous pipeline.
type SDELogicalChangeSourceProvider interface {
	OpenSDELogicalChangeSource(ctx context.Context, connInfo ConnectionInfo, path EngineCatalogPath, opts SDELogicalChangeOpenOptions) (SDELogicalChangeSource, error)
}

type SDELogicalChangeSource interface {
	Descriptor() SDELogicalSourceDescriptor
	Read(ctx context.Context, maxChanges int) (*SDELogicalChangeBatch, error)
	PositionRange(ctx context.Context) (earliest ChangeStreamPosition, latest ChangeStreamPosition, err error)
	Close(ctx context.Context) error
}

func ValidateSDELogicalSourceDescriptor(descriptor SDELogicalSourceDescriptor) error {
	if strings.TrimSpace(descriptor.RepositoryOwner) == "" || strings.TrimSpace(descriptor.RegistrationID) == "" {
		return errors.New("ArcGIS SDE logical source requires repository owner and registration ID")
	}
	if descriptor.VersioningModel != SDEVersioningModelTraditional {
		return fmt.Errorf("unsupported ArcGIS SDE versioning model %q", descriptor.VersioningModel)
	}
	if strings.TrimSpace(descriptor.VersionName) == "" {
		return errors.New("ArcGIS SDE logical source requires an explicit version name")
	}
	if len(descriptor.Fields) == 0 || len(descriptor.Keys) == 0 {
		return errors.New("ArcGIS SDE logical source requires fields and stable keys")
	}
	fields := make(map[string]datatype.FieldInfo, len(descriptor.Fields))
	for _, field := range descriptor.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" || field.Type == "" || field.Type == datatype.FieldTypeUnknown {
			return errors.New("ArcGIS SDE logical source contains an invalid field")
		}
		fields[strings.ToLower(name)] = field
	}
	for _, key := range descriptor.Keys {
		field, ok := fields[strings.ToLower(strings.TrimSpace(key))]
		if !ok || field.Nullable {
			return fmt.Errorf("ArcGIS SDE logical source key %q is missing or nullable", key)
		}
	}
	return nil
}

func ValidateSDELogicalPosition(position ChangeStreamPosition) error {
	if position.Type != SDELogicalPositionType || position.Version != SDELogicalPositionVersionV1 ||
		strings.TrimSpace(position.Partition) == "" || len(position.Values) == 0 {
		return errors.New("invalid ArcGIS SDE logical position")
	}
	for key, value := range position.Values {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return errors.New("invalid ArcGIS SDE logical position value")
		}
	}
	return nil
}

func ValidateSDELogicalChange(change SDELogicalChange, keys []string) error {
	if len(keys) == 0 {
		return errors.New("ArcGIS SDE logical change requires stable keys")
	}
	if change.Operation != TableChangeOperationUpsert && change.Operation != TableChangeOperationDelete {
		return fmt.Errorf("unsupported ArcGIS SDE logical change operation %q", change.Operation)
	}
	if err := ValidateSDELogicalPosition(change.Position); err != nil {
		return err
	}
	for _, key := range keys {
		value, ok := change.Key[key]
		if !ok || value == nil {
			return fmt.Errorf("ArcGIS SDE logical change is missing key %q", key)
		}
	}
	if change.Operation == TableChangeOperationUpsert && len(change.Row) == 0 {
		return errors.New("ArcGIS SDE upsert requires a business row")
	}
	if change.Operation == TableChangeOperationDelete && len(change.Row) != 0 {
		return errors.New("ArcGIS SDE delete must not carry a business row")
	}
	return nil
}

func ValidateSDELogicalChangeBatch(batch *SDELogicalChangeBatch, keys []string) error {
	if batch == nil {
		return errors.New("ArcGIS SDE logical change batch is required")
	}
	if err := ValidateSDELogicalPosition(batch.EndPosition); err != nil {
		return err
	}
	for _, change := range batch.Changes {
		if err := ValidateSDELogicalChange(change, keys); err != nil {
			return err
		}
		if change.Position.Partition != batch.EndPosition.Partition {
			return errors.New("ArcGIS SDE logical change batch crosses provider partitions")
		}
	}
	return nil
}
