package federatedquery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/addp/common/client"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

var ErrSourceItemInvalid = errors.New("invalid federated source item")

type Catalog struct {
	system *client.SystemServiceClient
	meta   *client.MetaClient
	logger *slog.Logger
}

type Source struct {
	EngineName string     `json:"engine_name"`
	EngineID   uint       `json:"engine_id"`
	EngineType string     `json:"engine_type"`
	Tables     []TableRef `json:"tables"`
}

type TableRef struct {
	EngineName   string   `json:"engine_name"`
	Schema       string   `json:"schema,omitempty"`
	Table        string   `json:"table"`
	ItemType     string   `json:"item_type"`
	Fields       []string `json:"fields,omitempty"`
	PhysicalPath string   `json:"physical_path,omitempty"`
	Format       string   `json:"format,omitempty"`
	Layout       string   `json:"layout,omitempty"`
}

type Candidate struct {
	Query    string
	EngineID uint
}

func NewCatalog(system *client.SystemServiceClient, meta *client.MetaClient) *Catalog {
	return &Catalog{
		system: system,
		meta:   meta,
		logger: slog.Default().With("component", "federated_query_catalog"),
	}
}

// SourceForItem resolves exactly the selected item, never a replacement table
// or a whole-source enumeration. Meta owns the physical name and ancestor facts.
func (c *Catalog) SourceForItem(ctx context.Context, tenantID, targetID uint, provider plugin.FederatedQueryRuntimeProvider, locator *resourcetree.ResourceLocator) ([]Source, error) {
	if c == nil || c.system == nil || c.meta == nil || tenantID == 0 || targetID == 0 || provider == nil || locator == nil || locator.EngineID == 0 || locator.EngineID == targetID || locator.ItemID == nil || *locator.ItemID == 0 || locator.NodeID != nil {
		return nil, ErrSourceItemInvalid
	}
	descriptor, err := c.system.WithTenantID(tenantID).GetEngineRuntimeDescriptor(ctx, locator.EngineID)
	if err != nil {
		return nil, err
	}
	caps := provider.Capabilities()
	if descriptor.ID != locator.EngineID || !engineselection.IsAvailableStorageEngine(descriptor.AsEngine()) || caps.Compute == nil || caps.Compute.Query == nil || caps.Compute.Query.Federation == nil {
		return nil, ErrSourceItemInvalid
	}
	supported := false
	for _, kind := range caps.Compute.Query.Federation.SourceEngineTypes {
		supported = supported || kind == descriptor.EngineType
	}
	if !supported {
		return nil, ErrSourceItemInvalid
	}
	facts, err := c.meta.WithTenantID(tenantID).GetItemAncestors(ctx, *locator.ItemID)
	if err != nil {
		return nil, err
	}
	registered, err := plugin.Get(descriptor.EngineType)
	if err != nil {
		return nil, ErrSourceItemInvalid
	}
	model, ok := registered.(plugin.EngineCatalogModelProvider)
	if !ok || facts == nil || string(locator.Type) != facts.Item.ItemType {
		return nil, ErrSourceItemInvalid
	}
	if _, err := resourcetree.CatalogPathFromMetaItem(model.EngineCatalogModel(), locator.EngineID, tenantID, *locator.ItemID, facts); err != nil {
		return nil, ErrSourceItemInvalid
	}
	tree := &models.MetadataTree{ChildNodes: facts.Ancestors, Items: []models.MetaItem{facts.Item}}
	source := Source{EngineID: descriptor.ID, EngineName: descriptor.Name, EngineType: descriptor.EngineType}
	if descriptor.EngineType == "minio" || descriptor.EngineType == "s3" {
		source.Tables = objectTables(descriptor.Name, tree)
	} else {
		source.Tables = relationalTables(descriptor.Name, descriptor.EngineType, tree)
	}
	if len(source.Tables) != 1 {
		return nil, ErrSourceItemInvalid
	}
	return []Source{source}, nil
}

func (c *Catalog) Sources(
	ctx context.Context,
	tenantID uint,
	runtimeEngineID uint,
	provider plugin.FederatedQueryRuntimeProvider,
) ([]Source, error) {
	if c == nil || c.system == nil || c.meta == nil || tenantID == 0 || runtimeEngineID == 0 || provider == nil {
		return nil, fmt.Errorf("联邦查询目录未正确初始化")
	}
	capabilities := provider.Capabilities()
	if capabilities.Compute == nil || capabilities.Compute.Query == nil || capabilities.Compute.Query.Federation == nil {
		return nil, fmt.Errorf("联邦查询 Runtime 未声明源引擎能力")
	}
	supported := make(map[string]struct{}, len(capabilities.Compute.Query.Federation.SourceEngineTypes))
	for _, engineType := range capabilities.Compute.Query.Federation.SourceEngineTypes {
		supported[engineType] = struct{}{}
	}
	descriptors, err := c.system.WithTenantID(tenantID).ListEngineRuntimeDescriptors(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取源引擎列表失败: %w", err)
	}

	sources := make([]Source, 0)
	for _, descriptor := range descriptors {
		if descriptor.ID == runtimeEngineID || !engineselection.IsAvailableStorageEngine(descriptor.AsEngine()) {
			continue
		}
		if _, ok := supported[descriptor.EngineType]; !ok {
			continue
		}
		source := Source{EngineName: descriptor.Name, EngineID: descriptor.ID, EngineType: descriptor.EngineType}
		tree, treeErr := c.meta.WithTenantID(tenantID).GetMetadataTree(ctx, descriptor.ID)
		if treeErr != nil {
			c.logger.WarnContext(ctx, "获取源引擎元数据树失败", "engine_id", descriptor.ID, "error", treeErr)
			continue
		}
		if descriptor.EngineType == "minio" || descriptor.EngineType == "s3" {
			source.Tables = objectTables(descriptor.Name, tree)
		} else {
			source.Tables = relationalTables(descriptor.Name, descriptor.EngineType, tree)
		}
		if len(source.Tables) > 0 {
			sort.Slice(source.Tables, func(i, j int) bool {
				if source.Tables[i].Schema == source.Tables[j].Schema {
					return source.Tables[i].Table < source.Tables[j].Table
				}
				return source.Tables[i].Schema < source.Tables[j].Schema
			})
			sources = append(sources, source)
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].EngineID < sources[j].EngineID })
	return sources, nil
}

func Candidates(sources []Source, limit int, quote string) ([]Candidate, error) {
	if quote != `"` && quote != "`" {
		return nil, fmt.Errorf("SQL identifier quote is required")
	}
	quoted := func(value string) string { return quote + strings.ReplaceAll(value, quote, quote+quote) + quote }
	candidates := make([]Candidate, 0)
	for _, source := range sources {
		for _, table := range source.Tables {
			parts := []string{quoted(SanitizeIdentifier(source.EngineName))}
			if table.Schema != "" {
				parts = append(parts, quoted(table.Schema))
			}
			name := table.Table
			if table.PhysicalPath != "" {
				name = SanitizeIdentifier(name)
			}
			parts = append(parts, quoted(name))
			query := fmt.Sprintf("SELECT *\nFROM %s", strings.Join(parts, "."))
			if limit > 0 {
				query += fmt.Sprintf("\nLIMIT %d", limit)
			}
			candidates = append(candidates, Candidate{
				Query:    query,
				EngineID: source.EngineID,
			})
		}
	}
	return candidates, nil
}

func objectTables(engineName string, tree *models.MetadataTree) []TableRef {
	if tree == nil {
		return nil
	}
	tables := make([]TableRef, 0)
	for _, item := range tree.Items {
		descriptor := dataitem.DescriptorFromAttributes(item.Attributes)
		if (item.ItemType != "object" && item.ItemType != "file") ||
			descriptor.DataType != datatype.Table || descriptor.Format != "parquet" {
			continue
		}
		tables = append(tables, TableRef{
			EngineName:   engineName,
			Table:        item.Name,
			ItemType:     "table",
			PhysicalPath: commonJSON.String(item.Attributes, "storage", "physical_path"),
			Format:       commonJSON.String(item.Attributes, "item", "format"),
			Layout:       commonJSON.String(item.Attributes, "item", "layout"),
		})
	}
	return tables
}

func relationalTables(engineName, engineType string, tree *models.MetadataTree) []TableRef {
	if tree == nil {
		return nil
	}
	registered, err := plugin.Get(engineType)
	if err != nil {
		return nil
	}
	model, ok := registered.(plugin.EngineCatalogModelProvider)
	if !ok {
		return nil
	}
	namespaceTerm := ""
	for _, level := range plugin.EngineCatalogBusinessLevels(model.EngineCatalogModel()) {
		if level.Role == plugin.EngineCatalogRoleBranch {
			if namespaceTerm != "" {
				return nil
			}
			namespaceTerm = level.Term
		}
	}
	nodes := make(map[uint]models.MetaNode)
	for _, node := range append(append([]models.MetaNode(nil), tree.TopNodes...), tree.ChildNodes...) {
		nodes[node.ID] = node
	}
	tables := make([]TableRef, 0)
	for _, item := range tree.Items {
		if item.ItemType != "table" {
			continue
		}
		namespace, exists := nodes[item.NodeID]
		if namespaceTerm == "" || !exists || namespace.NodeType != namespaceTerm || namespace.Name == "" {
			continue
		}
		tables = append(tables, TableRef{
			EngineName: engineName,
			Schema:     namespace.Name,
			Table:      item.Name,
			ItemType:   "table",
		})
	}
	return tables
}

func SanitizeIdentifier(value string) string {
	var result strings.Builder
	for _, char := range value {
		if char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			result.WriteRune(char)
		} else {
			result.WriteByte('_')
		}
	}
	value = result.String()
	if value == "" {
		return "engine"
	}
	if value[0] >= '0' && value[0] <= '9' {
		return "_" + value
	}
	return value
}
