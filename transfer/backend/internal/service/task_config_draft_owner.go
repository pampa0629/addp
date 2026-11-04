package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/addp/common/authorization"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	commonjson "github.com/addp/common/jsonmap"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/query"
	"github.com/addp/common/resourcetree"
)

var draftPermissions = []string{"transfer.task.create", "meta.catalog.read", "system.engine.read", "manager.data_item.read"}

type draftMetadata interface {
	GetItemSnapshot(context.Context, uint, uint, uint, string) (*commonmodels.MetaItem, error)
	GetItemAncestors(context.Context, uint) (*commonmodels.MetaItemAncestors, error)
	GetNodeByID(context.Context, uint) (*commonmodels.MetaNode, error)
	GetNodeByCatalogPath(context.Context, uint, string) (*commonmodels.MetaNode, error)
}

type TaskConfigDraftService struct {
	engines  ConfigCheckEngineFacts
	metadata func(uint) draftMetadata
}

type TaskConfigDraftResponse struct {
	Arguments map[string]interface{} `json:"arguments"`
}

func NewTaskConfigDraftService(engines ConfigCheckEngineFacts, metadata *commonclient.MetaClient) *TaskConfigDraftService {
	return &TaskConfigDraftService{engines: engines, metadata: func(tenant uint) draftMetadata { return metadata.WithTenantID(tenant) }}
}
func (s *TaskService) SetTaskConfigDrafter(drafter *TaskConfigDraftService) {
	s.configDrafter = drafter
}
func (s *TaskService) BuildTaskConfigDraft(ctx context.Context, subject authorization.AuthContext, body []byte) (*TaskConfigDraftResponse, error) {
	if s.configDrafter == nil {
		return nil, ErrConfigCheckDependency
	}
	return s.configDrafter.Build(ctx, subject, body)
}

func (s *TaskConfigDraftService) Build(ctx context.Context, subject authorization.AuthContext, body []byte) (*TaskConfigDraftResponse, error) {
	if authorization.ValidateAuthContext(subject) != nil || !subject.Token.ExpiresAt.After(time.Now().UTC()) || subject.Principal.Type != "user" || subject.Context.Type != "tenant" || subject.Context.TenantID == nil || !authorization.HasContextPermissions(subject, draftPermissions...) {
		return nil, ErrConfigCheckForbidden
	}
	switch subject.Token.Type {
	case "first_party_access_token", "oauth_access_token":
	case "delegated_access_token":
		if len(subject.Client.Audiences) != 1 || subject.Client.Audiences[0] != "transfer" || len(subject.Client.Scopes) != 1 || subject.Client.Scopes[0] != "transfer.task.draft.build" {
			return nil, ErrConfigCheckForbidden
		}
	default:
		return nil, ErrConfigCheckForbidden
	}
	tenant, err := strconv.ParseUint(*subject.Context.TenantID, 10, 32)
	if err != nil || tenant == 0 {
		return nil, ErrConfigCheckForbidden
	}
	if len(body) == 0 || len(body) > 512<<10 || !utf8.Valid(body) {
		return nil, ErrInvalidTaskConfig
	}
	var request TaskConfigDraftRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(interface{})) != io.EOF {
		return nil, ErrInvalidTaskConfig
	}
	source, err := resourcetree.ParseURI(request.SourceLocator)
	if err != nil || source.EngineID == 0 || source.Type != resourcetree.TypeCollection {
		return nil, ErrInvalidTaskConfig
	}
	parent, err := resourcetree.ParseURI(request.Target.ParentLocator)
	if err != nil || parent.EngineID == 0 || parent.Type != resourcetree.TypeSchema || len(parent.Path) != 1 {
		return nil, ErrInvalidTaskConfig
	}
	if s == nil || s.engines == nil || s.metadata == nil {
		return nil, ErrConfigCheckDependency
	}
	sourceEngine, err := s.engines.GetEngineForTenant(ctx, uint(tenant), source.EngineID)
	if err != nil || !draftEngineMatches(sourceEngine, uint(tenant), source.EngineID) {
		return nil, ErrConfigCheckDependency
	}
	targetEngine, err := s.engines.GetEngineForTenant(ctx, uint(tenant), parent.EngineID)
	if err != nil || !draftEngineMatches(targetEngine, uint(tenant), parent.EngineID) {
		return nil, ErrConfigCheckDependency
	}
	metadata := s.metadata(uint(tenant))
	if metadata == nil {
		return nil, ErrConfigCheckDependency
	}
	itemID := uint(0)
	if source.ItemID != nil {
		itemID = *source.ItemID
	}
	item, err := metadata.GetItemSnapshot(ctx, uint(tenant), source.EngineID, itemID, source.FullName())
	if err != nil || item == nil || item.ID == 0 || item.EngineID != source.EngineID || item.TenantID != uint(tenant) || item.FullName != source.FullName() || item.ItemType != "collection" || (itemID != 0 && item.ID != itemID) {
		return nil, ErrConfigCheckDependency
	}
	ancestors, err := metadata.GetItemAncestors(ctx, item.ID)
	if err != nil || ancestors == nil || configCheckHash(ancestors.Item) != configCheckHash(*item) {
		return nil, ErrConfigCheckDependency
	}
	registered, err := plugin.Get(sourceEngine.EngineType)
	if err != nil {
		return nil, ErrConfigCheckDependency
	}
	catalog, ok := registered.(plugin.EngineCatalogModelProvider)
	if !ok {
		return nil, ErrConfigCheckDependency
	}
	physical, err := resourcetree.CatalogPathFromMetaItem(catalog.EngineCatalogModel(), source.EngineID, uint(tenant), item.ID, ancestors)
	if err != nil {
		return nil, ErrConfigCheckDependency
	}
	parts := plugin.EngineCatalogPathWithoutRoot(physical).Segments
	if len(parts) == 0 {
		return nil, ErrConfigCheckDependency
	}
	// Flattened display full_name is ambiguous when names contain dots.
	// The exact URI segments must identify the same physical owner path.
	if len(parts) != len(source.Path) {
		return nil, draftError("draft_source_identity_mismatch", "/source_locator")
	}
	for i, part := range parts {
		if part.Name != source.Path[i] {
			return nil, draftError("draft_source_identity_mismatch", "/source_locator")
		}
	}
	var node *commonmodels.MetaNode
	if parent.NodeID != nil {
		node, err = metadata.GetNodeByID(ctx, *parent.NodeID)
	} else {
		node, err = metadata.GetNodeByCatalogPath(ctx, parent.EngineID, parent.FullName())
	}
	if err != nil || node == nil || node.ID == 0 || node.TenantID != uint(tenant) || node.EngineID != parent.EngineID || node.NodeType != "schema" || node.FullName != parent.FullName() || node.Name != parent.Path[0] || (parent.NodeID != nil && node.ID != *parent.NodeID) {
		return nil, ErrConfigCheckDependency
	}
	info := datatype.TableInfoFromPayload(commonjson.Section(item.Attributes, "type_info.table"), item.Name)
	args, err := buildTaskConfigDraft(request, taskConfigDraftFacts{sourceLocator: request.SourceLocator, targetParentLocator: request.Target.ParentLocator, sourceEngineType: sourceEngine.EngineType, targetEngineType: targetEngine.EngineType, source: info, mql: query.ResourceQueryReference{Parts: []string{parts[len(parts)-1].Name}}})
	if err != nil {
		return nil, err
	}
	return &TaskConfigDraftResponse{Arguments: args}, nil
}
func draftEngineMatches(engine *commonmodels.Engine, tenant, id uint) bool {
	return engine != nil && engine.ID == id && engine.TenantID != nil && *engine.TenantID == tenant && engine.DeletedAt == nil && engineselection.IsAvailable(engine)
}
