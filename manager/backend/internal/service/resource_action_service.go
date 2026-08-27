package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/engineaccess"
)

type ResourceActionService struct {
	systemClient SystemClient
}

type SystemClient interface {
	GetEngine(engineID uint) (*commonModels.Engine, error)
	GetEngineForTenant(ctx context.Context, tenantID, engineID uint) (*commonModels.Engine, error)
}

func NewResourceActionService(systemClient SystemClient) *ResourceActionService {
	return &ResourceActionService{systemClient: systemClient}
}

type ResourceActionsResponse struct {
	Locator        string                          `json:"locator"`
	Kind           string                          `json:"kind"`
	EngineCategory string                          `json:"engine_category"`
	Actions        map[string]ResourceActionStatus `json:"actions"`
}

type ResourceActionStatus struct {
	Supported bool     `json:"supported"`
	Reason    string   `json:"reason,omitempty"`
	DataTypes []string `json:"data_types,omitempty"`
	Formats   []string `json:"formats,omitempty"`
}

func (s *ResourceActionService) GetResourceActions(ctx context.Context, locatorURI string, tenantID *uint) (*ResourceActionsResponse, error) {
	_ = ctx
	locatorURI = strings.TrimSpace(locatorURI)
	if locatorURI == "" {
		return nil, fmt.Errorf("locator is required")
	}
	loc, err := resourcetree.ParseURI(locatorURI)
	if err != nil {
		return nil, err
	}
	if s == nil || s.systemClient == nil {
		return nil, fmt.Errorf("system client is required")
	}
	if tenantID == nil || *tenantID == 0 {
		return nil, ErrEngineAccessDenied
	}
	engine, err := s.systemClient.GetEngineForTenant(ctx, *tenantID, loc.EngineID)
	if err != nil {
		return nil, err
	}
	if !resourceBelongsToTenant(engine, tenantID) {
		return nil, ErrEngineAccessDenied
	}
	if err := engineaccess.EnsureAvailable(engine); err != nil {
		return nil, err
	}

	category := engineCategory(engine)
	kind := resourceActionKind(loc.Type)
	resp := &ResourceActionsResponse{
		Locator:        locatorURI,
		Kind:           kind,
		EngineCategory: category,
		Actions: map[string]ResourceActionStatus{
			"upload":   unsupported("upload only supports storage nodes"),
			"download": unsupported("download requires storage item"),
			"import":   unsupported("import only supports database nodes"),
			"export":   unsupported("export requires database item"),
		},
	}

	switch category {
	case "storage":
		if isStorageNodeType(loc.Type) && storageCanWrite(engine) {
			resp.Actions["upload"] = ResourceActionStatus{Supported: true}
		} else if isStorageNodeType(loc.Type) {
			resp.Actions["upload"] = unsupported("engine does not support content write")
		}
		if isStorageItemType(loc.Type) && storageCanRead(engine) {
			resp.Actions["download"] = ResourceActionStatus{Supported: true}
		} else if isStorageItemType(loc.Type) {
			resp.Actions["download"] = unsupported("engine does not support content read")
		}
	case "database":
		if isDatabaseNodeType(loc.Type) && databaseCanWrite(engine) {
			resp.Actions["import"] = ResourceActionStatus{
				Supported: true,
				DataTypes: []string{"table"},
				Formats:   tableImportFormats(),
			}
		} else if isDatabaseNodeType(loc.Type) {
			resp.Actions["import"] = unsupported("engine does not support table write")
		}
		if isDatabaseItemType(loc.Type) && databaseCanRead(engine) {
			resp.Actions["export"] = ResourceActionStatus{
				Supported: true,
				DataTypes: []string{"table"},
				Formats:   tableExportFormats(),
			}
		} else if isDatabaseItemType(loc.Type) {
			resp.Actions["export"] = unsupported("engine does not support table read")
		}
	}

	return resp, nil
}

func unsupported(reason string) ResourceActionStatus {
	return ResourceActionStatus{Supported: false, Reason: reason}
}

func resourceActionKind(t resourcetree.ResourceType) string {
	if isStorageItemType(t) || isDatabaseItemType(t) {
		return "item"
	}
	return "node"
}

func engineCategory(engine *commonModels.Engine) string {
	if engine == nil {
		return "unknown"
	}
	if caps, ok := engineCapabilities(engine); ok && caps.EngineFamily != "" {
		switch caps.EngineFamily {
		case "object", "file":
			return "storage"
		case "tabular", "dynamic_schema":
			return "database"
		}
	}
	if p, err := plugin.Get(engine.EngineType); err == nil {
		modelProvider, ok := p.(plugin.EngineCatalogModelProvider)
		if !ok {
			return "unknown"
		}
		switch strings.TrimSpace(modelProvider.EngineCatalogModel().RootTerm) {
		case plugin.EngineCatalogTermService, plugin.EngineCatalogTermRoot:
			return "storage"
		case plugin.EngineCatalogTermServer:
			return "database"
		}
	}
	return "unknown"
}

func engineCapabilities(engine *commonModels.Engine) (plugin.EngineCapabilities, bool) {
	if engine == nil || engine.Capabilities == nil || strings.TrimSpace(string(*engine.Capabilities)) == "" {
		if p, err := plugin.Get(engine.EngineType); err == nil {
			return p.Capabilities(), true
		}
		return plugin.EngineCapabilities{}, false
	}
	var caps plugin.EngineCapabilities
	if err := json.Unmarshal([]byte(string(*engine.Capabilities)), &caps); err != nil {
		if p, err := plugin.Get(engine.EngineType); err == nil {
			return p.Capabilities(), true
		}
		return plugin.EngineCapabilities{}, false
	}
	return caps, true
}

func storageCanRead(engine *commonModels.Engine) bool {
	caps, ok := engineCapabilities(engine)
	if !ok || caps.Storage == nil || caps.Storage.Store == nil {
		if p, err := plugin.Get(engine.EngineType); err == nil {
			_, ok := p.(plugin.ContentReadableProvider)
			return ok
		}
		return false
	}
	return caps.Storage.Store.StreamRead || caps.Storage.Store.RangeRead
}

func storageCanWrite(engine *commonModels.Engine) bool {
	caps, ok := engineCapabilities(engine)
	if !ok || caps.Storage == nil || caps.Storage.Store == nil {
		if p, err := plugin.Get(engine.EngineType); err == nil {
			_, ok := p.(plugin.ContentWritableProvider)
			return ok
		}
		return false
	}
	return caps.Storage.Store.StreamWrite
}

func databaseCanRead(engine *commonModels.Engine) bool {
	caps, ok := engineCapabilities(engine)
	if !ok || caps.Storage == nil || caps.Storage.Store == nil {
		if p, err := plugin.Get(engine.EngineType); err == nil {
			if _, ok := p.(plugin.TableReadSessionProvider); ok {
				return true
			}
			_, ok := p.(plugin.BatchReadableProvider)
			return ok
		}
		return false
	}
	return caps.Storage.Store.BatchRead || caps.Storage.Store.TableReadSession
}

func databaseCanWrite(engine *commonModels.Engine) bool {
	caps, ok := engineCapabilities(engine)
	if !ok || caps.Storage == nil || caps.Storage.Store == nil {
		if p, err := plugin.Get(engine.EngineType); err == nil {
			if _, ok := p.(plugin.TableWriteSessionProvider); ok {
				return true
			}
			_, ok := p.(plugin.BatchWritableProvider)
			return ok
		}
		return false
	}
	return caps.Storage.Store.BatchWrite || caps.Storage.Store.TableWriteSession
}

func isStorageNodeType(t resourcetree.ResourceType) bool {
	switch t {
	case resourcetree.TypeBucket, resourcetree.TypePrefix, resourcetree.TypeDirectory, resourcetree.TypeDir, resourcetree.TypeRoot, resourcetree.TypeService:
		return true
	default:
		return false
	}
}

func isStorageItemType(t resourcetree.ResourceType) bool {
	return t == resourcetree.TypeObject || t == resourcetree.TypeFile
}

func isDatabaseNodeType(t resourcetree.ResourceType) bool {
	switch t {
	case resourcetree.TypeDatabase, resourcetree.TypeSchema:
		return true
	default:
		return false
	}
}

func isDatabaseItemType(t resourcetree.ResourceType) bool {
	return t == resourcetree.TypeTable
}

func tableImportFormats() []string {
	formats := make([]string, 0)
	for _, snapshot := range format.ListFormatCapabilitySnapshots() {
		if snapshot.Descriptor.DataType != datatype.Table {
			continue
		}
		impl := snapshot.Implementations
		if impl.TableReaderProvider || impl.MultiTableReader || impl.ScopeTableReader {
			formats = append(formats, string(snapshot.Descriptor.Format))
		}
	}
	sort.Strings(formats)
	return formats
}

func tableExportFormats() []string {
	formats := make([]string, 0)
	for _, snapshot := range format.ListFormatCapabilitySnapshots() {
		if snapshot.Descriptor.DataType != datatype.Table {
			continue
		}
		impl := snapshot.Implementations
		if impl.TableWriterProvider || impl.MultiTableWriter {
			formats = append(formats, string(snapshot.Descriptor.Format))
		}
	}
	sort.Strings(formats)
	return formats
}
