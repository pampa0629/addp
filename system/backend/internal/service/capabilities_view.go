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
	capabilityStatusAddpPending       = "addp_pending"
)

func BuildCapabilitiesView(capabilitiesJSON *models.JSONString, engineType string) *models.CapabilitiesView {
	if capabilitiesJSON == nil || *capabilitiesJSON == "" {
		return nil
	}

	caps, err := engineplugin.ParseEngineCapabilities(string(*capabilitiesJSON))
	if err != nil || caps == nil {
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
	family := caps.EngineFamily
	if family == "" {
		family = inferEngineFamily(engineType)
	}
	if family != "" {
		badges = append(badges, models.CapabilityViewBadge{
			ID:       "engine_family",
			LabelKey: "system.engine.capabilityView.summary.engineFamily",
			ValueKey: capabilityValueKey("engineFamily", family),
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
	if hasTransferRead(caps) {
		badges = append(badges, capabilityBadge("transfer_read", "system.engine.capabilityView.summary.transferRead"))
	}
	if hasTransferWrite(caps) {
		badges = append(badges, capabilityBadge("transfer_write", "system.engine.capabilityView.summary.transferWrite"))
	}
	if hasPreview(caps) {
		badges = append(badges, capabilityBadge("preview", "system.engine.capabilityView.summary.preview"))
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
	if section := buildTransferSection(caps); section != nil {
		sections = append(sections, *section)
	}
	if section := buildPreviewSection(caps); section != nil {
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

	if caps.Storage.Metadata != nil && caps.Storage.Metadata.Supported {
		item := capabilityItem("metadata", "system.engine.capabilityView.items.metadata", capabilityStatusAvailable)
		item.Tags = appendTrueTags(item.Tags, map[string]bool{
			"field_schema":     caps.Storage.Metadata.FieldSchema,
			"statistics":       caps.Storage.Metadata.Statistics,
			"indexes":          caps.Storage.Metadata.Indexes,
			"constraints":      caps.Storage.Metadata.Constraints,
			"spatial_metadata": caps.Storage.Metadata.SpatialMetadata,
			"sampling":         caps.Storage.Metadata.Sampling,
			"native_metadata":  caps.Storage.Metadata.NativeMetadata,
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
		if level.Container {
			node.Tags = append(node.Tags, capabilityValueTag("container", "system.engine.capabilityView.values.container"))
		}
		if level.Item {
			node.Tags = append(node.Tags, capabilityValueTag("item", "system.engine.capabilityView.values.item"))
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
		section.Items = append(section.Items, item)
	} else if computeUnavailableForEngine(caps) {
		section.Items = append(section.Items, models.CapabilityViewItem{
			ID:        "query_unavailable",
			LabelKey:  "system.engine.capabilityView.items.query",
			ReasonKey: "system.engine.capabilityView.reasons.engineNoQuery",
			Status:    capabilityStatusEngineUnavailable,
		})
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

func buildTransferSection(caps *engineplugin.EngineCapabilities) *models.CapabilityViewSection {
	if caps.Transfer == nil {
		return buildPendingTransferSection(caps)
	}

	section := &models.CapabilityViewSection{
		ID:       "transfer",
		TitleKey: "system.engine.capabilityView.sections.transfer",
		Status:   capabilityStatusAvailable,
		Items:    []models.CapabilityViewItem{},
	}

	read := capabilityItem("transfer_read", "system.engine.capabilityView.items.transferRead", boolStatus(caps.Transfer.Read))
	if reader := caps.Transfer.ConnectorTypes["reader"]; reader != "" {
		read.Tags = append(read.Tags, models.CapabilityViewTag{ID: "reader", LabelKey: "system.engine.capabilityView.values.reader", Value: reader})
	}
	if caps.Transfer.StreamRead {
		read.Tags = append(read.Tags, capabilityValueTag("stream_read", "system.engine.capabilityView.values.streamRead"))
	}
	if caps.Transfer.ParallelRead {
		read.Tags = append(read.Tags, capabilityValueTag("parallel_read", "system.engine.capabilityView.values.parallelRead"))
	}
	section.Items = append(section.Items, read)

	write := capabilityItem("transfer_write", "system.engine.capabilityView.items.transferWrite", boolStatus(caps.Transfer.Write))
	if writer := caps.Transfer.ConnectorTypes["writer"]; writer != "" {
		write.Tags = append(write.Tags, models.CapabilityViewTag{ID: "writer", LabelKey: "system.engine.capabilityView.values.writer", Value: writer})
	}
	if caps.Transfer.BulkWrite {
		write.Tags = append(write.Tags, capabilityValueTag("bulk_write", "system.engine.capabilityView.values.bulkWrite"))
	}
	if caps.Transfer.ParallelWrite {
		write.Tags = append(write.Tags, capabilityValueTag("parallel_write", "system.engine.capabilityView.values.parallelWrite"))
	}
	section.Items = append(section.Items, write)
	if caps.Transfer.Checkpoint {
		section.Items = append(section.Items, capabilityItem("checkpoint", "system.engine.capabilityView.items.checkpoint", capabilityStatusAvailable))
	}

	section.Status = deriveSectionStatus(section.Items)
	return section
}

func buildPendingTransferSection(caps *engineplugin.EngineCapabilities) *models.CapabilityViewSection {
	if caps.Storage == nil {
		return nil
	}
	return &models.CapabilityViewSection{
		ID:       "transfer",
		TitleKey: "system.engine.capabilityView.sections.transfer",
		Status:   capabilityStatusAddpPending,
		Items: []models.CapabilityViewItem{{
			ID:        "transfer_pending",
			LabelKey:  "system.engine.capabilityView.items.transfer",
			ReasonKey: "system.engine.capabilityView.reasons.addpTransferPending",
			Status:    capabilityStatusAddpPending,
		}},
	}
}

func buildPreviewSection(caps *engineplugin.EngineCapabilities) *models.CapabilityViewSection {
	if caps.Preview == nil || !caps.Preview.Supported {
		if caps.Storage == nil {
			return nil
		}
		return &models.CapabilityViewSection{
			ID:       "preview",
			TitleKey: "system.engine.capabilityView.sections.preview",
			Status:   capabilityStatusAddpPending,
			Items: []models.CapabilityViewItem{{
				ID:        "preview_pending",
				LabelKey:  "system.engine.capabilityView.items.preview",
				ReasonKey: "system.engine.capabilityView.reasons.addpPreviewPending",
				Status:    capabilityStatusAddpPending,
			}},
		}
	}

	section := &models.CapabilityViewSection{
		ID:       "preview",
		TitleKey: "system.engine.capabilityView.sections.preview",
		Status:   capabilityStatusAvailable,
		Items:    []models.CapabilityViewItem{},
	}

	item := capabilityItem("preview", "system.engine.capabilityView.items.preview", capabilityStatusAvailable)
	item.Tags = valueTags("mode", caps.Preview.Modes)
	if caps.Preview.MaxRows > 0 {
		item.Tags = append(item.Tags, models.CapabilityViewTag{ID: "max_rows", LabelKey: "system.engine.capabilityView.values.maxRows", Value: strconv.Itoa(caps.Preview.MaxRows)})
	}
	if caps.Preview.MaxBytes > 0 {
		item.Tags = append(item.Tags, models.CapabilityViewTag{ID: "max_bytes", LabelKey: "system.engine.capabilityView.values.maxBytes", Value: formatBytes(caps.Preview.MaxBytes)})
	}
	if caps.Preview.DirectPreview {
		item.Tags = append(item.Tags, capabilityValueTag("direct_preview", "system.engine.capabilityView.values.directPreview"))
	}
	section.Items = append(section.Items, item)
	return section
}

func buildExtensionsSection(caps *engineplugin.EngineCapabilities) *models.CapabilityViewSection {
	if len(caps.Extensions) == 0 {
		return nil
	}
	return &models.CapabilityViewSection{
		ID:       "extensions",
		TitleKey: "system.engine.capabilityView.sections.extensions",
		Status:   capabilityStatusAvailable,
		Items:    extensionItems(caps.Extensions),
	}
}

func extensionItems(extensions map[string]interface{}) []models.CapabilityViewItem {
	keys := sortedMapKeys(extensions)
	items := make([]models.CapabilityViewItem, 0, len(keys))
	for _, key := range keys {
		items = append(items, models.CapabilityViewItem{
			ID:       key,
			LabelKey: capabilityValueKey("extensions", key),
			Value:    primitiveString(extensions[key]),
			Status:   capabilityStatusAvailable,
			Tags:     extensionTags(extensions[key]),
		})
	}
	return items
}

func extensionTags(value interface{}) []models.CapabilityViewTag {
	obj, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	keys := sortedMapKeys(obj)
	tags := make([]models.CapabilityViewTag, 0, len(keys))
	for _, key := range keys {
		tags = append(tags, models.CapabilityViewTag{ID: key, Value: fmt.Sprintf("%s: %s", key, primitiveString(obj[key]))})
	}
	return tags
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
		return r == '_' || r == '-' || r == '.'
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

func boolStatus(enabled bool) string {
	if enabled {
		return capabilityStatusAvailable
	}
	return capabilityStatusAddpPending
}

func deriveSectionStatus(items []models.CapabilityViewItem) string {
	hasAvailable := false
	hasPending := false
	for _, item := range items {
		switch item.Status {
		case capabilityStatusAvailable:
			hasAvailable = true
		case capabilityStatusAddpPending:
			hasPending = true
		}
	}
	if hasAvailable {
		return capabilityStatusAvailable
	}
	if hasPending {
		return capabilityStatusAddpPending
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

func hasTransferRead(caps *engineplugin.EngineCapabilities) bool {
	return caps.Transfer != nil && caps.Transfer.Read
}

func hasTransferWrite(caps *engineplugin.EngineCapabilities) bool {
	return caps.Transfer != nil && caps.Transfer.Write
}

func hasPreview(caps *engineplugin.EngineCapabilities) bool {
	return caps.Preview != nil && caps.Preview.Supported
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

func computeUnavailableForEngine(caps *engineplugin.EngineCapabilities) bool {
	switch caps.EngineFamily {
	case "object", "file":
		return true
	default:
		return false
	}
}

func inferEngineFamily(engineType string) string {
	switch engineType {
	case "minio", "s3":
		return "object"
	case "nfs":
		return "file"
	default:
		return ""
	}
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
