package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonjson "github.com/addp/common/jsonmap"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/resourcetree"
	"github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
)

type metricTableMetadata struct {
	Version       int64
	Locator, Name string
	Path          plugin.EngineCatalogPath
	Fields        map[string]datatype.FieldInfo
}
type metricPlanMetadata struct {
	Engine *commonmodels.EngineRuntimeDescriptor
	Tables map[int64]metricTableMetadata
}

// All remote metadata is read before local aggregate locks. The locked resolver
// checks versions/locators again, then binds only the fields used by the plan.
func (s *MetricImplementationService) metricSourceMetadata(ctx context.Context, item *models.MetricImplementation, contract models.MetricContract) (*metricPlanMetadata, error) {
	engine, err := s.metricEngineDescriptor(ctx, item)
	if err != nil {
		return nil, err
	}
	provider, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, invalidRequest()
	}
	catalog, ok := provider.(plugin.EngineCatalogModelProvider)
	if !ok {
		return nil, invalidRequest()
	}
	if s.meta == nil {
		return nil, apperrors.Unavailable("metric_metadata_unavailable", i18n.MsgMetricMetadataUnavailable)
	}
	ids := map[int64]bool{item.FactTableID: true}
	relationIDs := metricContractRelationIDs(contract)
	var relations []models.TableRelation
	if err := s.repo.DB().WithContext(ctx).Where("tenant_id = ? AND source_table = ? AND id IN ?", item.TenantID, item.FactTableID, relationIDs).Find(&relations).Error; err != nil {
		return nil, err
	}
	if len(relations) != len(relationIDs) {
		return nil, invalidRequest()
	}
	for _, r := range relations {
		ids[r.TargetTable] = true
	}
	result := &metricPlanMetadata{Engine: engine, Tables: map[int64]metricTableMetadata{}}
	for id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		table, err := s.tableRepo.GetByID(id, item.TenantID)
		if err != nil {
			return nil, metricNotFound()
		}
		uri, _ := table.Materialization["target_parent_locator"].(string)
		name, _ := table.Materialization["target_name"].(string)
		locator, err := resourcetree.ParseURI(uri)
		if err != nil || locator.EngineID != engine.ID || name == "" {
			return nil, invalidRequest()
		}
		leaf := *locator
		leaf.Path = append(append([]string(nil), locator.Path...), name)
		leaf.Type = resourcetree.TypeTable
		leaf.NodeID = nil
		leaf.ItemID = nil
		path, err := resourcetree.EngineCatalogPathFromLocator(catalog.EngineCatalogModel(), &leaf)
		if err != nil {
			return nil, invalidRequest()
		}
		identity, err := resourcetree.DataItemIdentityFromCatalogPath(catalog.EngineCatalogModel(), path)
		if err != nil {
			return nil, invalidRequest()
		}
		metadata, err := s.meta.WithTenantID(uint(item.TenantID)).GetItemByCatalogPath(engine.ID, identity.FullName)
		if err != nil {
			return nil, apperrors.Wrap(apperrors.KindUnavailable, "metric_metadata_unavailable", i18n.MsgMetricMetadataUnavailable, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if metadata == nil || metadata.ID == 0 || metadata.TenantID != uint(item.TenantID) || metadata.EngineID != engine.ID || metadata.FullName != identity.FullName || metadata.ScannedAt == nil {
			return nil, metricMetadataIncomplete()
		}
		// Decode facts without display normalization: identifier whitespace is data.
		payload := commonjson.Section(metadata.Attributes, "type_info.table")
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, metricMetadataIncomplete()
		}
		var info datatype.TableInfo
		if err = json.Unmarshal(raw, &info); err != nil || len(info.Fields) == 0 {
			return nil, metricMetadataIncomplete()
		}
		fields := map[string]datatype.FieldInfo{}
		for _, f := range info.Fields {
			if f.Name == "" {
				return nil, metricMetadataIncomplete()
			}
			if _, dup := fields[f.Name]; dup {
				return nil, metricMetadataIncomplete()
			}
			if len(f.Path) == 0 {
				f.Path = []string{f.Name}
			}
			fields[f.Name] = datatype.FieldInfo{Name: f.Name, Path: append([]string(nil), f.Path...), Type: f.Type, NativeType: f.NativeType, Nullable: f.Nullable, Size: f.Size, Precision: f.Precision, Scale: f.Scale}
		}
		result.Tables[id] = metricTableMetadata{Version: table.Version, Locator: uri, Name: name, Path: path, Fields: fields}
	}
	return result, nil
}
func metricMetadataIncomplete() error {
	return apperrors.Validation("metric_metadata_incomplete", i18n.MsgMetricMetadataIncomplete)
}
func metricContractRelationIDs(c models.MetricContract) []int64 {
	ids := map[int64]metricPlanRelation{c.SubjectRelationID: {}}
	refs := []models.MetricFieldReference{c.Subject, c.Distinct, c.Time}
	for _, f := range c.Filters {
		refs = append(refs, f.Field)
	}
	for _, r := range refs {
		if r.RelationID > 0 {
			ids[r.RelationID] = metricPlanRelation{}
		}
	}
	return sortedMetricRelationIDs(ids)
}
func bindMetricSources(p plan.Plan, bindings metricPlanBindings) ([]plugin.SourceBinding, error) {
	sources := map[string]metricPlanSource{"source_0": bindings.Fact}
	for id, r := range bindings.Relations {
		sources[fmt.Sprintf("source_%d", id)] = r.Target
	}
	var result []plugin.SourceBinding
	for _, node := range p.Nodes {
		if node.Scan == nil {
			continue
		}
		source, ok := sources[string(node.Scan.Source)]
		if !ok {
			return nil, invalidRequest()
		}
		bound := plugin.SourceBinding{Source: node.Scan.Source, Path: source.Metadata.Path}
		for _, logical := range node.Scan.Fields {
			index := strings.LastIndex(logical.Name, "_f")
			if index < 0 {
				return nil, invalidRequest()
			}
			var fieldID int64
			if _, err := fmt.Sscanf(logical.Name[index+2:], "%d", &fieldID); err != nil {
				return nil, invalidRequest()
			}
			f, ok := source.Metadata.Fields[source.Fields[fieldID].ColumnName]
			if !ok || f.NativeType == "" || f.Type != logical.Type {
				return nil, metricMetadataIncomplete()
			}
			bound.Columns = append(bound.Columns, plugin.ColumnBinding{Column: logical.Name, Field: f})
		}
		result = append(result, bound)
	}
	return result, nil
}
