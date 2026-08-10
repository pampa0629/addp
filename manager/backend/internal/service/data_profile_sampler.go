package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
)

var (
	ErrDataProfileUnsupported    = errors.New("data profiling is not supported for this resource")
	ErrDataProfileUnavailable    = errors.New("data profiling is unavailable")
	ErrDataProfileInvalidRequest = errors.New("invalid data profiling request")
	ErrDataProfileSourceChanged  = errors.New("data profile source structure changed")
)

type DataProfileSelection struct {
	ChildName       string `json:"child_name,omitempty"`
	RefPath         string `json:"ref_path,omitempty"`
	NestedChildPath string `json:"nested_child_path,omitempty"`
}

type DataProfileTarget struct {
	Locator            string
	Selection          DataProfileSelection
	ItemFingerprint    string
	ItemID             *uint
	EngineID           uint
	SourceVersion      string
	DependencySnapshot map[string]interface{}
	RowCount           *int64
	RowCountExact      bool
	Fields             []datatype.FieldInfo
	ConditionSupported bool
	resolved           *preview.PreviewResolverRequest
}

type DataProfileSample struct {
	Rows          []map[string]interface{}
	Fields        []datatype.FieldInfo
	RowsScanned   int64
	Truncated     bool
	Partial       bool
	RowCount      *int64
	RowCountExact bool
}

type DataProfileBudget struct {
	SampleSize     int
	MaxRowsScanned int
	PageSize       int
	Timeout        time.Duration
}

var DefaultDataProfileBudget = DataProfileBudget{
	SampleSize:     2000,
	MaxRowsScanned: 10000,
	PageSize:       500,
	Timeout:        2 * time.Minute,
}

type DataProfileSampleProvider interface {
	ResolveTarget(context.Context, uint, string, DataProfileSelection) (*DataProfileTarget, error)
	Sample(context.Context, *DataProfileTarget, dataprofile.DataScope, DataProfileBudget) (*DataProfileSample, error)
}

// PreviewDataProfileSampleProvider reuses the registered engine/format data
// readers, but owns an independent server-side sampling window and reservoir.
// It never consumes rows held by the browser preview page.
type PreviewDataProfileSampleProvider struct {
	resolver   *preview.PreviewResolver
	metaClient *commonClient.MetaClient
}

func NewPreviewDataProfileSampleProvider(
	resolver *preview.PreviewResolver,
	metaClient *commonClient.MetaClient,
) *PreviewDataProfileSampleProvider {
	return &PreviewDataProfileSampleProvider{resolver: resolver, metaClient: metaClient}
}

func (p *PreviewDataProfileSampleProvider) ResolveTarget(
	ctx context.Context,
	tenantID uint,
	locator string,
	selection DataProfileSelection,
) (*DataProfileTarget, error) {
	if p == nil || p.resolver == nil || p.metaClient == nil {
		return nil, ErrDataProfileUnavailable
	}
	locator = strings.TrimSpace(locator)
	if locator == "" {
		return nil, errors.New("locator is required")
	}
	resolved, err := p.resolver.ResolveRequestFromURIWithSelection(
		ctx,
		locator,
		1,
		1,
		strings.TrimSpace(selection.ChildName),
		strings.Trim(strings.TrimSpace(selection.RefPath), "/"),
		strings.Trim(strings.TrimSpace(selection.NestedChildPath), "/"),
		plugin.GraphSampleFilter{},
		&tenantID,
	)
	if err != nil {
		return nil, err
	}
	if resolved.MetaItemID == nil || strings.TrimSpace(resolved.ItemFingerprint) == "" {
		return nil, fmt.Errorf("%w: a scanned data item is required", ErrDataProfileUnsupported)
	}
	if !selectionTargetsChild(selection) && dataTypeFromAttributes(resolved.Metadata.Attributes) != string(datatype.Table) {
		return nil, ErrDataProfileUnsupported
	}

	item, err := p.metaClient.WithTenantID(tenantID).GetItemByID(*resolved.MetaItemID)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve profile source item: %v", ErrDataProfileUnavailable, err)
	}
	if item == nil || item.TenantID != tenantID {
		return nil, ErrDataProfileUnsupported
	}
	itemFingerprint := commonModels.GenerateItemFingerprint(item.EngineID, item.FullName)
	if itemFingerprint != resolved.ItemFingerprint {
		return nil, errors.New("resolved item fingerprint does not match Meta item")
	}
	sourceVersion := sourceVersionForItem(itemFingerprint, *item)
	dependencySnapshot := map[string]interface{}{
		"item_id":          item.ID,
		"item_fingerprint": itemFingerprint,
		"source_version":   sourceVersion,
	}
	if item.DataUpdatedAt != nil {
		dependencySnapshot["data_updated_at"] = item.DataUpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if item.SizeBytes != nil {
		dependencySnapshot["size_bytes"] = *item.SizeBytes
	}
	if selectionTargetsChild(selection) {
		dependencySnapshot["selection"] = selection
	}
	fields := profileFieldsFromAttributes(item.Attributes)
	conditionSupported := false
	if !selectionTargetsChild(selection) && len(fields) > 0 && resolved.Engine != nil {
		if plug, pluginErr := plugin.Get(resolved.Engine.EngineType); pluginErr == nil {
			parameterized, parameterizedOK := plug.(plugin.ParameterizedSQLQueryRuntimeProvider)
			_, batchReadable := plug.(plugin.BatchReadableProvider)
			conditionSupported = batchReadable && parameterizedOK && parameterized.SupportsParameterizedQueries()
		}
	}

	return &DataProfileTarget{
		Locator:            locator,
		Selection:          normalizeDataProfileSelection(selection),
		ItemFingerprint:    itemFingerprint,
		ItemID:             resolved.MetaItemID,
		EngineID:           item.EngineID,
		SourceVersion:      sourceVersion,
		DependencySnapshot: dependencySnapshot,
		RowCount:           item.RowCount,
		RowCountExact:      item.RowCount != nil,
		Fields:             fields,
		ConditionSupported: conditionSupported,
		resolved:           resolved,
	}, nil
}

func (p *PreviewDataProfileSampleProvider) Sample(
	ctx context.Context,
	target *DataProfileTarget,
	dataScope dataprofile.DataScope,
	budget DataProfileBudget,
) (*DataProfileSample, error) {
	if p == nil || p.resolver == nil || target == nil || target.resolved == nil {
		return nil, ErrDataProfileUnavailable
	}
	budget = normalizeDataProfileBudget(budget)
	if dataScope.Kind == dataprofile.DataScopeKindCondition && !target.ConditionSupported {
		return nil, fmt.Errorf("%w: conditional profiling is not supported", ErrDataProfileUnsupported)
	}
	reservoir := make([]map[string]interface{}, 0, budget.SampleSize)
	rng := rand.New(rand.NewSource(stableProfileSampleSeed(target, dataScope)))
	rowsScanned := int64(0)
	fields := []datatype.FieldInfo(nil)
	total := int64(-1)
	pages := []int{1}
	visited := map[int]struct{}{}

	for len(pages) > 0 && rowsScanned < int64(budget.MaxRowsScanned) {
		page := pages[0]
		pages = pages[1:]
		if _, seen := visited[page]; seen {
			continue
		}
		visited[page] = struct{}{}

		request := *target.resolved
		request.Pagination = &preview.Pagination{Page: page, PageSize: budget.PageSize}
		request.DataScope = dataScope
		result, err := p.resolver.Preview(ctx, &request)
		if err != nil {
			return nil, err
		}
		table, ok := result.Data.(*models.TablePreview)
		if !ok || table == nil || result.PreviewType != preview.PreviewModeTable {
			return nil, ErrDataProfileUnsupported
		}
		if len(fields) == 0 {
			if !profileFieldsMatchColumns(table) {
				return nil, ErrDataProfileSourceChanged
			}
			fields = profileFieldsFromPreview(table)
		}
		if table.Total >= 0 {
			total = int64(table.Total)
		}
		for _, row := range table.Rows {
			if rowsScanned >= int64(budget.MaxRowsScanned) {
				break
			}
			rowsScanned++
			if len(reservoir) < budget.SampleSize {
				reservoir = append(reservoir, cloneProfileRow(row))
				continue
			}
			index := rng.Int63n(rowsScanned)
			if index < int64(budget.SampleSize) {
				reservoir[index] = cloneProfileRow(row)
			}
		}

		if page == 1 && total >= 0 {
			pages = append(pages, profileSamplePages(total, budget.PageSize, budget.MaxRowsScanned)...)
		} else if total < 0 && len(table.Rows) >= budget.PageSize {
			pages = append(pages, page+1)
		}
		if len(table.Rows) == 0 || (total >= 0 && rowsScanned >= total) {
			break
		}
	}

	if fields == nil {
		fields = []datatype.FieldInfo{}
	}
	truncated := total > rowsScanned || rowsScanned >= int64(budget.MaxRowsScanned)
	rowCount := target.RowCount
	rowCountExact := target.RowCountExact
	if dataScope.Kind == dataprofile.DataScopeKindCondition {
		rowCount = nil
		rowCountExact = false
		if !truncated && total < 0 {
			matched := rowsScanned
			rowCount = &matched
			rowCountExact = true
		}
	}
	return &DataProfileSample{
		Rows:          reservoir,
		Fields:        fields,
		RowsScanned:   rowsScanned,
		Truncated:     truncated,
		RowCount:      rowCount,
		RowCountExact: rowCountExact,
	}, nil
}

func normalizeDataProfileBudget(budget DataProfileBudget) DataProfileBudget {
	if budget.SampleSize <= 0 {
		budget.SampleSize = DefaultDataProfileBudget.SampleSize
	}
	if budget.MaxRowsScanned < budget.SampleSize {
		budget.MaxRowsScanned = max(budget.SampleSize, DefaultDataProfileBudget.MaxRowsScanned)
	}
	if budget.PageSize <= 0 {
		budget.PageSize = DefaultDataProfileBudget.PageSize
	}
	if budget.PageSize > budget.MaxRowsScanned {
		budget.PageSize = budget.MaxRowsScanned
	}
	if budget.Timeout <= 0 {
		budget.Timeout = DefaultDataProfileBudget.Timeout
	}
	return budget
}

func profileSamplePages(total int64, pageSize, maxRows int) []int {
	if total <= int64(pageSize) {
		return nil
	}
	blockCount := maxRows / pageSize
	if blockCount <= 1 {
		return nil
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages <= blockCount {
		pages := make([]int, 0, totalPages-1)
		for page := 2; page <= totalPages; page++ {
			pages = append(pages, page)
		}
		return pages
	}
	pages := make([]int, 0, blockCount-1)
	for i := 1; i < blockCount; i++ {
		page := 1 + int((int64(totalPages-1)*int64(i))/int64(blockCount-1))
		pages = append(pages, page)
	}
	return pages
}

func profileFieldsFromPreview(table *models.TablePreview) []datatype.FieldInfo {
	if table == nil {
		return nil
	}
	return append([]datatype.FieldInfo(nil), table.Fields...)
}

func profileFieldsMatchColumns(table *models.TablePreview) bool {
	if table == nil || len(table.Fields) == 0 || len(table.Columns) != len(table.Fields) {
		return false
	}
	for index, field := range table.Fields {
		if field.Name != table.Columns[index] {
			return false
		}
	}
	return true
}

func dataTypeFromAttributes(attributes map[string]interface{}) string {
	item, _ := attributes["item"].(map[string]interface{})
	return strings.ToLower(strings.TrimSpace(fmt.Sprint(item["data_type"])))
}

func cloneProfileRow(row map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(row))
	for key, value := range row {
		cloned[key] = value
	}
	return cloned
}

func stableProfileSampleSeed(target *DataProfileTarget, dataScope dataprofile.DataScope) int64 {
	payload, _ := json.Marshal(struct {
		Fingerprint string                `json:"fingerprint"`
		Version     string                `json:"version"`
		Selection   DataProfileSelection  `json:"selection"`
		DataScope   dataprofile.DataScope `json:"data_scope"`
	}{target.ItemFingerprint, target.SourceVersion, target.Selection, dataScope})
	hash := sha256.Sum256(payload)
	var seed int64
	for _, value := range hash[:8] {
		seed = seed<<8 | int64(value)
	}
	return seed
}

func normalizeDataProfileSelection(selection DataProfileSelection) DataProfileSelection {
	selection.ChildName = strings.TrimSpace(selection.ChildName)
	selection.RefPath = strings.Trim(strings.TrimSpace(selection.RefPath), "/")
	selection.NestedChildPath = strings.Trim(strings.TrimSpace(selection.NestedChildPath), "/")
	return selection
}

func selectionTargetsChild(selection DataProfileSelection) bool {
	selection = normalizeDataProfileSelection(selection)
	return selection.ChildName != "" || selection.RefPath != "" || selection.NestedChildPath != ""
}

func profileTargetKey(tenantID uint, locator string, selection DataProfileSelection, configHash string) string {
	payload, _ := json.Marshal(struct {
		TenantID   uint                 `json:"tenant_id"`
		Locator    string               `json:"locator"`
		Selection  DataProfileSelection `json:"selection"`
		ConfigHash string               `json:"profile_config_hash"`
	}{tenantID, strings.TrimSpace(locator), normalizeDataProfileSelection(selection), strings.TrimSpace(configHash)})
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

func profileFieldsFromAttributes(attributes map[string]interface{}) []datatype.FieldInfo {
	table := datatype.TableInfoFromPayload(commonJSON.Section(attributes, "type_info.table"), "")
	if table == nil {
		return nil
	}
	return append([]datatype.FieldInfo(nil), table.Fields...)
}
