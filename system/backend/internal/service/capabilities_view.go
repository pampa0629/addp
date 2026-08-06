package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/system/internal/models"
)

const (
	capabilityStatusAvailable         = "available"
	capabilityStatusEngineUnavailable = "engine_unavailable"
	capabilityStatusInstalled         = "installed"
	capabilityStatusNotInstalled      = "not_installed"
	capabilityStatusUnknown           = "unknown"
)

func BuildCapabilitiesView(capabilitiesJSON *models.JSONString, engineType string) *models.CapabilitiesView {
	if capabilitiesJSON == nil || *capabilitiesJSON == "" {
		return nil
	}

	caps, err := engineplugin.ParseEngineCapabilities(string(*capabilitiesJSON))
	if err != nil || caps == nil {
		return nil
	}
	if caps.SchemaVersion != engineplugin.CapabilitiesSchemaVersion {
		return nil
	}

	view := &models.CapabilitiesView{
		Summary:  buildCapabilitySummary(caps, engineType),
		Sections: buildCapabilitySections(caps),
		JSONView: buildCapabilitiesJSONView(*capabilitiesJSON),
	}
	return view
}

func buildCapabilitySummary(caps *engineplugin.EngineCapabilities, engineType string) []models.CapabilityViewBadge {
	badges := make([]models.CapabilityViewBadge, 0, 8)
	if caps.EngineFamily != "" {
		badges = append(badges, models.CapabilityViewBadge{
			ID:       "engine_family",
			LabelKey: "system.engine.capabilityView.summary.engineFamily",
			ValueKey: capabilityValueKey("engineFamily", caps.EngineFamily),
			Status:   capabilityStatusAvailable,
		})
	}

	if hasCatalog(caps) {
		badges = append(badges, capabilityBadge("catalog", "system.engine.capabilityView.summary.catalog"))
	}
	if hasAnyStoreRead(caps) {
		badges = append(badges, capabilityBadge("content_read", "system.engine.capabilityView.summary.contentRead"))
	}
	if caps.Storage != nil && caps.Storage.Store != nil && caps.Storage.Store.RangeRead {
		badges = append(badges, capabilityBadge("range_read", "system.engine.capabilityView.summary.rangeRead"))
	}
	if hasQuery(caps) {
		badges = append(badges, capabilityBadge("query", "system.engine.capabilityView.summary.query"))
	}
	if hasWorkflow(caps) {
		badges = append(badges, capabilityBadge("workflow", "system.engine.capabilityView.summary.workflow"))
	}
	if hasScript(caps) {
		badges = append(badges, capabilityBadge("script", "system.engine.capabilityView.summary.script"))
	}
	return badges
}

func capabilityBadge(id, labelKey string) models.CapabilityViewBadge {
	return models.CapabilityViewBadge{ID: id, LabelKey: labelKey, Status: capabilityStatusAvailable}
}

func buildCapabilitySections(caps *engineplugin.EngineCapabilities) []models.CapabilityViewSection {
	sections := make([]models.CapabilityViewSection, 0, 6)

	if section := buildStorageSection(caps); section != nil {
		sections = append(sections, *section)
	}
	if section := buildComputeSection(caps); section != nil {
		sections = append(sections, *section)
	}
	if section := buildExtensionsSection(caps); section != nil {
		sections = append(sections, *section)
	}

	return sections
}

func buildStorageSection(caps *engineplugin.EngineCapabilities) *models.CapabilityViewSection {
	if caps.Storage == nil {
		return nil
	}

	section := &models.CapabilityViewSection{
		ID:       "storage",
		TitleKey: "system.engine.capabilityView.sections.storage",
		Status:   capabilityStatusAvailable,
		Path:     buildCatalogPath(caps.Storage.CatalogModel),
		Items:    []models.CapabilityViewItem{},
	}

	if caps.Storage.Catalog != nil && caps.Storage.Catalog.Supported {
		item := capabilityItem("catalog", "system.engine.capabilityView.items.catalog", capabilityStatusAvailable)
		if caps.Storage.Catalog.RealTime {
			item.Tags = append(item.Tags, capabilityValueTag("real_time", "system.engine.capabilityView.values.realTime"))
		}
		item.Tags = append(item.Tags, valueTags("kind", caps.Storage.Catalog.NodeKinds)...)
		if caps.Storage.Catalog.SupportsSearch {
			item.Tags = append(item.Tags, capabilityValueTag("search", "system.engine.capabilityView.values.search"))
		}
		if caps.Storage.Catalog.SupportsFilter {
			item.Tags = append(item.Tags, capabilityValueTag("filter", "system.engine.capabilityView.values.filter"))
		}
		if caps.Storage.Catalog.SystemFiltering {
			item.Tags = append(item.Tags, capabilityValueTag("system_filtering", "system.engine.capabilityView.values.systemFiltering"))
		}
		section.Items = append(section.Items, item)
	}

	if caps.Storage.Facts != nil && caps.Storage.Facts.Supported {
		item := capabilityItem("facts", "system.engine.capabilityView.items.facts", capabilityStatusAvailable)
		item.Tags = appendTrueTags(item.Tags, map[string]bool{
			"field_info":    caps.Storage.Facts.FieldInfo,
			"statistics":    caps.Storage.Facts.Statistics,
			"indexes":       caps.Storage.Facts.Indexes,
			"constraints":   caps.Storage.Facts.Constraints,
			"spatial_facts": caps.Storage.Facts.SpatialFacts,
			"sampling":      caps.Storage.Facts.Sampling,
			"native_facts":  caps.Storage.Facts.NativeFacts,
		})
		section.Items = append(section.Items, item)
	}

	if caps.Storage.Store != nil {
		readTags := make([]models.CapabilityViewTag, 0, 3)
		readTags = appendBoolTag(readTags, "stream_read", caps.Storage.Store.StreamRead)
		readTags = appendBoolTag(readTags, "range_read", caps.Storage.Store.RangeRead)
		readTags = appendBoolTag(readTags, "batch_read", caps.Storage.Store.BatchRead)
		if len(readTags) > 0 {
			item := capabilityItem("content_read", "system.engine.capabilityView.items.contentRead", capabilityStatusAvailable)
			item.Tags = readTags
			section.Items = append(section.Items, item)
		}

		writeTags := make([]models.CapabilityViewTag, 0, 3)
		writeTags = appendBoolTag(writeTags, "stream_write", caps.Storage.Store.StreamWrite)
		writeTags = appendBoolTag(writeTags, "range_write", caps.Storage.Store.RangeWrite)
		writeTags = appendBoolTag(writeTags, "batch_write", caps.Storage.Store.BatchWrite)
		if len(writeTags) > 0 {
			item := capabilityItem("content_write", "system.engine.capabilityView.items.contentWrite", capabilityStatusAvailable)
			item.Tags = writeTags
			section.Items = append(section.Items, item)
		}

		tableTags := make([]models.CapabilityViewTag, 0, 8)
		tableTags = appendBoolTag(tableTags, "table_read_session", caps.Storage.Store.TableReadSession)
		tableTags = appendBoolTag(tableTags, "table_write_session", caps.Storage.Store.TableWriteSession)
		tableTags = appendBoolTag(tableTags, "table_write_prepare", caps.Storage.Store.TableWritePrepare)
		if caps.Storage.Store.TableReadSpatialTransform {
			tableTags = append(tableTags, capabilityValueTag("table_read_spatial_transform", "system.engine.capabilityView.values.tableReadSpatialTransform"))
		}
		if spatialEncoding := caps.Storage.Store.TableSpatialEncoding; spatialEncoding != nil {
			tableTags = append(tableTags, valueTags("geometry_read_encoding", spatialEncoding.GeometryReadEncodings)...)
			tableTags = append(tableTags, valueTags("geometry_write_encoding", spatialEncoding.GeometryWriteEncodings)...)
			if spatialEncoding.ReadTransform {
				tableTags = append(tableTags, capabilityValueTag("spatial_read_transform", "system.engine.capabilityView.values.spatialReadTransform"))
			}
			if spatialEncoding.WriteTransform {
				tableTags = append(tableTags, capabilityValueTag("spatial_write_transform", "system.engine.capabilityView.values.spatialWriteTransform"))
			}
			if spatialEncoding.NativeSpatialFunctions {
				tableTags = append(tableTags, capabilityValueTag("native_spatial_functions", "system.engine.capabilityView.values.nativeSpatialFunctions"))
			}
		}
		if len(tableTags) > 0 {
			item := capabilityItem("table_io", "system.engine.capabilityView.items.tableIO", capabilityStatusAvailable)
			item.Tags = tableTags
			section.Items = append(section.Items, item)
		}
	}

	if len(caps.Storage.Semantics) > 0 {
		item := capabilityItem("semantics", "system.engine.capabilityView.items.semantics", capabilityStatusAvailable)
		item.Tags = valueTags("semantic", caps.Storage.Semantics)
		section.Items = append(section.Items, item)
	}

	if len(caps.Storage.NotSupported) > 0 {
		item := capabilityItem("not_supported", "system.engine.capabilityView.items.notSupported", capabilityStatusEngineUnavailable)
		item.Tags = valueTags("not_supported", caps.Storage.NotSupported)
		section.Items = append(section.Items, item)
	}

	return section
}

func buildCatalogPath(model *engineplugin.CatalogModelSpec) []models.CapabilityPathNode {
	if model == nil {
		return nil
	}

	path := make([]models.CapabilityPathNode, 0, len(model.Levels)+1)
	if model.RootTerm != "" {
		path = append(path, models.CapabilityPathNode{
			ID:       "root",
			LabelKey: capabilityValueKey("catalog", model.RootTerm),
			Value:    model.RootTerm,
		})
	}
	for _, level := range model.Levels {
		node := models.CapabilityPathNode{
			ID:       level.Term,
			LabelKey: capabilityValueKey("catalog", level.Term),
			Value:    level.Term,
		}
		switch level.Role {
		case engineplugin.CatalogRoleBranch:
			node.Tags = append(node.Tags, capabilityValueTag("branch", "system.engine.capabilityView.values.branch"))
		case engineplugin.CatalogRoleLeaf:
			node.Tags = append(node.Tags, capabilityValueTag("leaf", "system.engine.capabilityView.values.leaf"))
		}
		if level.Optional {
			node.Tags = append(node.Tags, capabilityValueTag("optional", "system.engine.capabilityView.values.optional"))
		}
		node.Tags = append(node.Tags, valueTags("kind", level.Kinds)...)
		path = append(path, node)
	}
	return path
}

func buildComputeSection(caps *engineplugin.EngineCapabilities) *models.CapabilityViewSection {
	section := &models.CapabilityViewSection{
		ID:       "compute",
		TitleKey: "system.engine.capabilityView.sections.compute",
		Items:    []models.CapabilityViewItem{},
	}

	if hasQuery(caps) {
		query := caps.Compute.Query
		item := capabilityItem("query", "system.engine.capabilityView.items.query", capabilityStatusAvailable)
		item.Tags = append(item.Tags, valueTags("language", query.Languages)...)
		item.Tags = append(item.Tags, valueTags("result_kind", query.ResultKinds)...)
		if query.DefaultLanguage != "" {
			item.Tags = append(item.Tags, models.CapabilityViewTag{ID: "default_language", LabelKey: "system.engine.capabilityView.values.defaultLanguage", Value: query.DefaultLanguage})
		}
		if query.ReadOnly {
			item.Tags = append(item.Tags, capabilityValueTag("read_only", "system.engine.capabilityView.values.readOnly"))
		}
		if query.SupportsExplain {
			item.Tags = append(item.Tags, capabilityValueTag("query_plan", "system.engine.capabilityView.values.queryPlan"))
		}
		if query.SupportsCancel {
			item.Tags = append(item.Tags, capabilityValueTag("cancel", "system.engine.capabilityView.values.cancel"))
		}
		if query.Parameters != nil && query.Parameters.Supported {
			item.Tags = append(item.Tags, capabilityValueTag("query_parameters", "system.engine.capabilityView.values.queryParameters"))
			item.Tags = append(item.Tags, valueTags("parameter_type", query.Parameters.Types)...)
		}
		section.Items = append(section.Items, item)
	}

	if hasWorkflow(caps) {
		workflow := caps.Compute.Workflow
		item := capabilityItem("workflow", "system.engine.capabilityView.items.workflow", capabilityStatusAvailable)
		item.Tags = append(item.Tags, valueTags("operator_mode", workflow.SupportedOperatorMode)...)
		if workflow.DynamicOperators {
			item.Tags = append(item.Tags, capabilityValueTag("dynamic_operators", "system.engine.capabilityView.values.dynamicOperators"))
		}
		section.Items = append(section.Items, item)
	}

	if hasScript(caps) {
		script := caps.Compute.Script
		item := capabilityItem("script", "system.engine.capabilityView.items.script", capabilityStatusAvailable)
		item.Tags = append(item.Tags, valueTags("mode", script.Modes)...)
		item.Tags = append(item.Tags, valueTags("language", script.Languages)...)
		section.Items = append(section.Items, item)
	}

	if len(section.Items) == 0 {
		return nil
	}
	section.Status = deriveSectionStatus(section.Items)
	return section
}

func buildExtensionsSection(caps *engineplugin.EngineCapabilities) *models.CapabilityViewSection {
	if len(caps.Extensions) == 0 {
		return nil
	}
	items := extensionItems(caps.Extensions)
	if len(items) == 0 {
		return nil
	}
	return &models.CapabilityViewSection{
		ID:       "extensions",
		TitleKey: "system.engine.capabilityView.sections.extensionStatus",
		Status:   deriveSectionStatus(items),
		Items:    items,
	}
}

func extensionItems(extensions map[string]interface{}) []models.CapabilityViewItem {
	keys := sortedMapKeys(extensions)
	items := make([]models.CapabilityViewItem, 0, len(keys)*2)
	for _, key := range keys {
		if key == "postgresql" {
			if postgresqlItems := postgresqlExtensionItems(extensions[key]); len(postgresqlItems) > 0 {
				items = append(items, postgresqlItems...)
				continue
			}
		}
		if key == engineplugin.EngineExtensionSpatialWorkspaces {
			if workspaceItems := spatialWorkspaceItems(extensions[key]); len(workspaceItems) > 0 {
				items = append(items, workspaceItems...)
				continue
			}
		}
		items = append(items, models.CapabilityViewItem{
			ID:       key,
			LabelKey: capabilityValueKey("extensions", key),
			Status:   capabilityStatusAvailable,
			Tags:     extensionTags(extensions[key]),
		})
	}
	return items
}

func spatialWorkspaceItems(value interface{}) []models.CapabilityViewItem {
	workspaces, ok := value.([]interface{})
	if !ok {
		if typed, ok := value.([]engineplugin.SpatialWorkspaceFact); ok {
			workspaces = make([]interface{}, 0, len(typed))
			for _, workspace := range typed {
				workspaces = append(workspaces, workspace)
			}
		} else {
			return nil
		}
	}

	items := make([]models.CapabilityViewItem, 0, len(workspaces))
	for i, raw := range workspaces {
		workspace, ok := spatialWorkspaceMap(raw)
		if !ok {
			continue
		}
		item := models.CapabilityViewItem{
			ID:       spatialWorkspaceItemID(workspace, i),
			LabelKey: spatialWorkspaceLabelKey(workspace),
			Status:   spatialWorkspaceStatus(stringValue(workspace["state"])),
		}
		if ecosystem := stringValue(workspace["ecosystem"]); ecosystem != "" {
			item.Tags = append(item.Tags, models.CapabilityViewTag{
				ID:       "ecosystem_" + capabilityKeySegment(ecosystem),
				LabelKey: capabilityValueKey("values", "ecosystem"),
				Value:    ecosystem,
			})
		}
		if kind := stringValue(workspace["kind"]); kind != "" {
			item.Tags = append(item.Tags, models.CapabilityViewTag{
				ID:       "kind_" + capabilityKeySegment(kind),
				LabelKey: capabilityValueKey("values", "kind"),
				Value:    kind,
			})
		}
		if state := stringValue(workspace["state"]); state != "" {
			item.Tags = append(item.Tags, models.CapabilityViewTag{
				ID:       "state_" + capabilityKeySegment(state),
				LabelKey: capabilityValueKey("values", "state"),
				Value:    state,
			})
		}
		if backend := stringValue(workspace["backend_engine_type"]); backend != "" {
			item.Tags = append(item.Tags, models.CapabilityViewTag{
				ID:       "backend_engine_type",
				LabelKey: capabilityValueKey("values", "backend_engine_type"),
				Value:    backend,
			})
		}
		if boundID := primitiveString(workspace["bound_runtime_engine_id"]); boundID != "" && boundID != "0" {
			item.Tags = append(item.Tags, models.CapabilityViewTag{
				ID:       "bound_runtime_engine_id",
				LabelKey: capabilityValueKey("values", "bound_runtime_engine_id"),
				Value:    boundID,
			})
		}
		if risk := stringValue(workspace["risk_level"]); risk != "" {
			item.Tags = append(item.Tags, models.CapabilityViewTag{
				ID:       "risk_level_" + capabilityKeySegment(risk),
				LabelKey: capabilityValueKey("values", "risk_level"),
				Value:    risk,
			})
		}
		if canEnable, exists := workspace["can_enable"]; exists {
			item.Tags = append(item.Tags, boolStateTag("can_enable", canEnable))
		}
		if evidence, ok := asMap(workspace["evidence"]); ok {
			item.Tags = append(item.Tags, capabilityValueTag("evidence", capabilityValueKey("values", "evidence")))
			for _, key := range sortedMapKeys(evidence) {
				item.Tags = append(item.Tags, models.CapabilityViewTag{
					ID:       "evidence_" + capabilityKeySegment(key),
					LabelKey: capabilityValueKey("values", key),
					Value:    primitiveString(evidence[key]),
				})
			}
		}
		items = append(items, item)
	}
	return items
}

func spatialWorkspaceLabelKey(workspace map[string]interface{}) string {
	ecosystem := strings.ToLower(strings.TrimSpace(stringValue(workspace["ecosystem"])))
	kind := strings.ToLower(strings.TrimSpace(stringValue(workspace["kind"])))
	switch ecosystem + "/" + kind {
	case "supermap/sdx_postgis":
		return capabilityValueKey("extensions", "supermap_sdx_postgis")
	case "supermap/sdx_postgresql":
		return capabilityValueKey("extensions", "supermap_sdx_postgresql")
	case "arcgis/sde":
		return capabilityValueKey("extensions", "arcgis_sde")
	default:
		return capabilityValueKey("extensions", "spatial_workspace")
	}
}

func spatialWorkspaceMap(value interface{}) (map[string]interface{}, bool) {
	if obj, ok := asMap(value); ok {
		return obj, true
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, false
	}
	return obj, true
}

func spatialWorkspaceItemID(workspace map[string]interface{}, index int) string {
	ecosystem := capabilityKeySegment(stringValue(workspace["ecosystem"]))
	kind := capabilityKeySegment(stringValue(workspace["kind"]))
	if ecosystem == "" {
		ecosystem = "workspace"
	}
	if kind == "" {
		kind = "item"
	}
	return "spatial_workspace_" + ecosystem + "_" + kind + "_" + strconv.Itoa(index)
}

func spatialWorkspaceStatus(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "detected", "enabled":
		return capabilityStatusAvailable
	case "not_detected":
		return capabilityStatusNotInstalled
	case "permission_denied":
		return capabilityStatusEngineUnavailable
	default:
		return capabilityStatusUnknown
	}
}

func postgresqlExtensionItems(value interface{}) []models.CapabilityViewItem {
	obj, ok := asMap(value)
	if !ok {
		return nil
	}

	items := make([]models.CapabilityViewItem, 0, 4)
	server := capabilityItem("postgresql_server", capabilityValueKey("extensions", "postgresql_server"), capabilityStatusAvailable)
	if version := stringValue(obj["server_version"]); version != "" {
		server.Value = version
	}
	if versionNum := primitiveString(obj["server_version_num"]); versionNum != "" && versionNum != "0" {
		server.Tags = append(server.Tags, models.CapabilityViewTag{
			ID:       "server_version_num",
			LabelKey: capabilityValueKey("values", "server_version_num"),
			Value:    versionNum,
		})
	}
	items = append(items, server)

	if postgis, ok := asMap(obj["postgis"]); ok {
		item := capabilityItem("postgis", capabilityValueKey("extensions", "postgis"), extensionStatus(postgis))
		if version := stringValue(postgis["version"]); version != "" {
			item.Value = version
		}
		item.Tags = extensionStateTags(postgis)
		items = append(items, item)
	}

	if pgvector, ok := asMap(obj["pgvector"]); ok {
		item := capabilityItem("pgvector", capabilityValueKey("extensions", "pgvector"), extensionStatus(pgvector))
		if version := stringValue(pgvector["version"]); version != "" {
			item.Value = version
		}
		item.Tags = extensionStateTags(pgvector)
		items = append(items, item)
	}

	extraExtensions := postgresqlExtraExtensionTags(obj)
	if len(extraExtensions) > 0 {
		items = append(items, models.CapabilityViewItem{
			ID:       "postgresql_extra_extensions",
			LabelKey: capabilityValueKey("extensions", "postgresql_extra_extensions"),
			Status:   capabilityStatusInstalled,
			Tags:     extraExtensions,
		})
	}

	return items
}

func postgresqlExtraExtensionTags(obj map[string]interface{}) []models.CapabilityViewTag {
	extensionLabels := map[string]string{
		"postgis_topology":       "PostGIS Topology",
		"postgis_tiger_geocoder": "PostGIS Tiger Geocoder",
	}
	extensionIDs := []string{"postgis_topology", "postgis_tiger_geocoder"}
	tags := make([]models.CapabilityViewTag, 0, len(extensionIDs))
	for _, extensionID := range extensionIDs {
		extension, ok := asMap(obj[extensionID])
		if !ok || !boolValue(extension["installed"]) {
			continue
		}
		value := extensionLabels[extensionID]
		if version := stringValue(extension["version"]); version != "" {
			value = fmt.Sprintf("%s %s", value, version)
		}
		tags = append(tags, models.CapabilityViewTag{
			ID:    extensionID,
			Value: value,
		})
	}
	return tags
}

func extensionStatus(extension map[string]interface{}) string {
	if boolValue(extension["installed"]) || boolValue(extension["type_available"]) {
		return capabilityStatusAvailable
	}
	return capabilityStatusNotInstalled
}

func extensionStateTags(extension map[string]interface{}) []models.CapabilityViewTag {
	tags := make([]models.CapabilityViewTag, 0, 6)
	if !boolValue(extension["installed"]) && !boolValue(extension["type_available"]) {
		return nil
	}
	if boolValue(extension["installed"]) {
		tags = append(tags, capabilityValueTag("installed", capabilityValueKey("values", "installed")))
	}
	if _, exists := extension["available"]; exists {
		tags = append(tags, boolStateTag("available", extension["available"]))
	}
	if schema := stringValue(extension["schema"]); schema != "" {
		tags = append(tags, models.CapabilityViewTag{ID: "schema", LabelKey: capabilityValueKey("values", "schema"), Value: schema})
	}
	if boolValue(extension["type_available"]) {
		tags = append(tags, capabilityValueTag("type_available", capabilityValueKey("values", "type_available")))
	}
	if boolValue(extension["st_extent"]) {
		tags = append(tags, capabilityValueTag("st_extent", capabilityValueKey("values", "st_extent")))
	}
	if boolValue(extension["st_transform"]) {
		tags = append(tags, capabilityValueTag("st_transform", capabilityValueKey("values", "st_transform")))
	}
	return tags
}

func boolStateTag(id string, value interface{}) models.CapabilityViewTag {
	if boolValue(value) {
		return capabilityValueTag(id, capabilityValueKey("values", id))
	}
	return capabilityValueTag(id+"_false", capabilityValueKey("values", id+"_false"))
}

func extensionTags(value interface{}) []models.CapabilityViewTag {
	obj, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	keys := sortedMapKeys(obj)
	tags := make([]models.CapabilityViewTag, 0, len(keys))
	for _, key := range keys {
		if _, nested := asMap(obj[key]); nested {
			continue
		}
		tags = append(tags, models.CapabilityViewTag{
			ID:       key,
			LabelKey: capabilityValueKey("values", key),
			Value:    primitiveString(obj[key]),
		})
	}
	return tags
}

func asMap(value interface{}) (map[string]interface{}, bool) {
	obj, ok := value.(map[string]interface{})
	return obj, ok
}

func boolValue(value interface{}) bool {
	typed, ok := value.(bool)
	return ok && typed
}

func stringValue(value interface{}) string {
	typed, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(typed)
}

func buildCapabilitiesJSONView(capabilitiesJSON models.JSONString) []models.CapabilityJSONNode {
	var value interface{}
	if err := json.Unmarshal([]byte(capabilitiesJSON), &value); err != nil {
		return nil
	}
	children := buildJSONNodes(value)
	if children == nil {
		return []models.CapabilityJSONNode{{Key: "capabilities", Value: primitiveString(value)}}
	}
	return children
}

func buildJSONNodes(value interface{}) []models.CapabilityJSONNode {
	switch typed := value.(type) {
	case map[string]interface{}:
		keys := sortedMapKeys(typed)
		nodes := make([]models.CapabilityJSONNode, 0, len(keys))
		for _, key := range keys {
			node := models.CapabilityJSONNode{Key: key}
			if children := buildJSONNodes(typed[key]); children != nil {
				node.Children = children
			} else {
				node.Value = primitiveString(typed[key])
			}
			nodes = append(nodes, node)
		}
		return nodes
	case []interface{}:
		nodes := make([]models.CapabilityJSONNode, 0, len(typed))
		for i, item := range typed {
			node := models.CapabilityJSONNode{Key: strconv.Itoa(i)}
			if children := buildJSONNodes(item); children != nil {
				node.Children = children
			} else {
				node.Value = primitiveString(item)
			}
			nodes = append(nodes, node)
		}
		return nodes
	default:
		return nil
	}
}

func capabilityItem(id, labelKey, status string) models.CapabilityViewItem {
	return models.CapabilityViewItem{ID: id, LabelKey: labelKey, Status: status}
}

func capabilityValueTag(id, labelKey string) models.CapabilityViewTag {
	return models.CapabilityViewTag{ID: id, LabelKey: labelKey}
}

func appendTrueTags(tags []models.CapabilityViewTag, values map[string]bool) []models.CapabilityViewTag {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if values[key] {
			tags = append(tags, capabilityValueTag(key, capabilityValueKey("values", key)))
		}
	}
	return tags
}

func appendBoolTag(tags []models.CapabilityViewTag, id string, enabled bool) []models.CapabilityViewTag {
	if !enabled {
		return tags
	}
	return append(tags, capabilityValueTag(id, capabilityValueKey("values", id)))
}

func valueTags(prefix string, values []string) []models.CapabilityViewTag {
	tags := make([]models.CapabilityViewTag, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		tags = append(tags, models.CapabilityViewTag{
			ID:       prefix + "_" + value,
			LabelKey: capabilityValueKey(prefix, value),
			Value:    value,
		})
	}
	return tags
}

func capabilityValueKey(namespace, value string) string {
	normalized := capabilityKeySegment(value)
	return "system.engine.capabilityView." + namespace + "." + normalized
}

func capabilityKeySegment(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || r == '+'
	})
	if len(parts) == 0 {
		return value
	}
	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}
	result := parts[0]
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		result += strings.ToUpper(part[:1]) + part[1:]
	}
	return result
}

func deriveSectionStatus(items []models.CapabilityViewItem) string {
	hasInstalled := false
	for _, item := range items {
		switch item.Status {
		case capabilityStatusAvailable:
			return capabilityStatusAvailable
		case capabilityStatusInstalled:
			hasInstalled = true
		}
	}
	if hasInstalled {
		return capabilityStatusInstalled
	}
	return capabilityStatusEngineUnavailable
}

func hasCatalog(caps *engineplugin.EngineCapabilities) bool {
	return caps.Storage != nil && caps.Storage.Catalog != nil && caps.Storage.Catalog.Supported
}

func hasAnyStoreRead(caps *engineplugin.EngineCapabilities) bool {
	if caps.Storage == nil || caps.Storage.Store == nil {
		return false
	}
	store := caps.Storage.Store
	return store.StreamRead || store.RangeRead || store.BatchRead
}

func hasQuery(caps *engineplugin.EngineCapabilities) bool {
	return caps.Compute != nil && caps.Compute.Query != nil && caps.Compute.Query.Supported
}

func hasWorkflow(caps *engineplugin.EngineCapabilities) bool {
	return caps.Compute != nil && caps.Compute.Workflow != nil && caps.Compute.Workflow.Supported
}

func hasScript(caps *engineplugin.EngineCapabilities) bool {
	return caps.Compute != nil && caps.Compute.Script != nil && caps.Compute.Script.Supported
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func primitiveString(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}
		return string(data)
	}
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
