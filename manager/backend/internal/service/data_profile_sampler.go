package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/preview"
)

var (
	ErrDataProfileUnsupported                 = errors.New("data profiling is not supported for this resource")
	ErrDataProfileUnavailable                 = errors.New("data profiling is unavailable")
	ErrDataProfileInvalidRequest              = errors.New("invalid data profiling request")
	ErrDataProfileSourceChanged               = errors.New("data profile source structure changed")
	ErrDataProfileProtectionRequired          = errors.New("data profiling protection is required")
	ErrDataProfileSourceAuthorizationRequired = errors.New("data profiling source authorization is required")
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
	ReadSet       *plugin.QueryReadSet
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
	MaxRowsScanned: preview.MaxProfilePreparedRows,
	PageSize:       500,
	Timeout:        2 * time.Minute,
}

type DataProfileSampleProvider interface {
	ResolveTarget(context.Context, uint, string, DataProfileSelection) (*DataProfileTarget, error)
	Sample(context.Context, *DataProfileTarget, dataprofile.DataScope, DataProfileBudget, *DataProfileSamplePlan, func(context.Context) error) (*DataProfileSample, error)
}

// PreviewDataProfileSampleProvider consumes only the already-bound plan, never
// the interactive preview or an unchecked ReadBatch route.
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
			conditionSupported = parameterizedOK && parameterized.SupportsParameterizedQueries()
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
	plan *DataProfileSamplePlan,
	beforeRead func(context.Context) error,
) (*DataProfileSample, error) {
	if p == nil || p.resolver == nil || target == nil || target.resolved == nil {
		return nil, ErrDataProfileUnavailable
	}
	if plan == nil || plan.pages == nil || beforeRead == nil {
		return nil, ErrDataProfileSourceAuthorizationRequired
	}
	if err := validateSingleTableProfilePlan(plan, target.EngineID); err != nil {
		return nil, err
	}
	positions, err := dataProfilePagePositions(target.RowCount, dataScope, budget)
	if err != nil || !reflect.DeepEqual(positions, plan.Positions()) {
		return nil, ErrDataProfileSourceChanged
	}
	boundSet, err := canonicalProfileReadSet(plan.ReadSet(), target.EngineID)
	if err != nil || len(target.Fields) == 0 {
		return nil, ErrDataProfileSourceAuthorizationRequired
	}
	columns := make([]string, len(target.Fields))
	stable := false
	for i, field := range target.Fields {
		columns[i] = field.Name
		stable = stable || field.PrimaryKey
	}
	sample := &DataProfileSample{Fields: append([]datatype.FieldInfo(nil), target.Fields...)}
	var readPaths []plugin.EngineCatalogPath
	for i, page := range positions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		query, err := plan.pages.Query(i)
		if err != nil || query == nil {
			return nil, ErrDataProfileSourceAuthorizationRequired
		}
		pageSet, err := query.ReadSet(ctx)
		if err != nil {
			return nil, err
		}
		canonical, err := canonicalProfileReadSet(pageSet, target.EngineID)
		if err != nil {
			return nil, ErrDataProfileSourceAuthorizationRequired
		}
		union, err := plugin.NewQueryReadSet(append(append([]plugin.EngineCatalogPath(nil), boundSet.Paths...), canonical.Paths...)...)
		if err != nil || !reflect.DeepEqual(union, boundSet) {
			return nil, ErrDataProfileSourceChanged
		}
		if err := beforeRead(ctx); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := query.Execute(ctx)
		if err != nil {
			return nil, err
		}
		if result == nil || len(result.Rows) > page.Limit || !reflect.DeepEqual(result.Columns, columns) {
			return nil, ErrDataProfileSourceChanged
		}
		readPaths = append(readPaths, canonical.Paths...)
		for _, row := range result.Rows {
			if len(row) != len(columns) {
				return nil, ErrDataProfileSourceChanged
			}
			for _, name := range columns {
				if _, exists := row[name]; !exists {
					return nil, ErrDataProfileSourceChanged
				}
			}
			sample.RowsScanned++
			if len(sample.Rows) < budget.SampleSize {
				sample.Rows = append(sample.Rows, row)
			} else if index := rand.Int64N(sample.RowsScanned); index < int64(budget.SampleSize) {
				sample.Rows[index] = row
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sample.ReadSet, err = plugin.NewQueryReadSet(readPaths...)
	if err != nil || !reflect.DeepEqual(sample.ReadSet, boundSet) {
		return nil, ErrDataProfileSourceChanged
	}
	if dataScope.Kind == dataprofile.DataScopeKindAll && target.RowCount != nil {
		count := *target.RowCount
		sample.RowCount, sample.RowCountExact = &count, target.RowCountExact
	}
	sample.Partial = !stable || !sample.RowCountExact
	sample.Truncated = sample.RowsScanned >= int64(budget.MaxRowsScanned) && (sample.RowCount == nil || *sample.RowCount > sample.RowsScanned)
	return sample, nil
}

func dataTypeFromAttributes(attributes map[string]interface{}) string {
	item, _ := attributes["item"].(map[string]interface{})
	return strings.ToLower(strings.TrimSpace(fmt.Sprint(item["data_type"])))
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

func profileTargetKey(actor dataProfileActor, locator string, selection DataProfileSelection, configHash string) string {
	payload, _ := json.Marshal(struct {
		Actor      dataProfileActor     `json:"actor"`
		Locator    string               `json:"locator"`
		Selection  DataProfileSelection `json:"selection"`
		ConfigHash string               `json:"profile_config_hash"`
	}{actor, strings.TrimSpace(locator), normalizeDataProfileSelection(selection), strings.TrimSpace(configHash)})
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
