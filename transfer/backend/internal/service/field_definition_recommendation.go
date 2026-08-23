package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	"github.com/addp/common/format"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

var (
	ErrFieldRecommendationInvalid     = errors.New("invalid field definition recommendation request")
	ErrFieldRecommendationUnsupported = errors.New("field definition recommendation is unsupported")
	ErrFieldRecommendationUnavailable = errors.New("field definition recommendation is unavailable")
)

type FieldDefinitionRecommendationRequest struct {
	SourceLocator    string   `json:"source_locator" binding:"required"`
	SourceFields     []string `json:"source_fields" binding:"required"`
	TargetEngineType string   `json:"target_engine_type" binding:"required"`
}

type DecimalFieldRecommendation struct {
	SourceField  string `json:"source_field"`
	Precision    int    `json:"precision"`
	Scale        int    `json:"scale"`
	NonNullCount int64  `json:"non_null_count"`
	FitsTarget   bool   `json:"fits_target"`
}

type FieldDefinitionRecommendationResult struct {
	TargetEngineType string                       `json:"target_engine_type"`
	Basis            string                       `json:"basis"`
	RowsScanned      int64                        `json:"rows_scanned"`
	Fields           []DecimalFieldRecommendation `json:"fields"`
}

type fieldRecommendationEngineGetter interface {
	GetEngineForTenant(ctx context.Context, tenantID, engineID uint) (*commonmodels.Engine, error)
}

type FieldDefinitionRecommendationService struct {
	engines fieldRecommendationEngineGetter
}

func NewFieldDefinitionRecommendationService(engines fieldRecommendationEngineGetter) *FieldDefinitionRecommendationService {
	return &FieldDefinitionRecommendationService{engines: engines}
}

func (s *FieldDefinitionRecommendationService) Recommend(
	ctx context.Context,
	tenantID uint,
	request FieldDefinitionRecommendationRequest,
) (*FieldDefinitionRecommendationResult, error) {
	if s == nil || s.engines == nil {
		return nil, ErrFieldRecommendationUnavailable
	}
	if !strings.EqualFold(strings.TrimSpace(request.TargetEngineType), "mysql") {
		return nil, fmt.Errorf("%w: target engine must be mysql", ErrFieldRecommendationUnsupported)
	}
	fields, err := normalizedRecommendationFields(request.SourceFields)
	if err != nil {
		return nil, err
	}
	locator, err := resourcetree.ParseURI(strings.TrimSpace(request.SourceLocator))
	if err != nil || locator.EngineID == 0 || locator.Type != resourcetree.TypeTable || locator.ItemID == nil {
		return nil, fmt.Errorf("%w: source_locator must identify a scanned table item", ErrFieldRecommendationInvalid)
	}
	engine, err := s.engines.GetEngineForTenant(ctx, tenantID, locator.EngineID)
	if err != nil || !engineselection.IsAvailable(engine) {
		return nil, fmt.Errorf("%w: source engine is unavailable", ErrFieldRecommendationUnavailable)
	}
	if engine.TenantID != nil && *engine.TenantID != tenantID {
		return nil, fmt.Errorf("%w: source engine is outside the current tenant", ErrFieldRecommendationInvalid)
	}
	plug, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFieldRecommendationUnsupported, err)
	}
	modelProvider, modelOK := plug.(plugin.CatalogModelProvider)
	reader, readOK := plug.(plugin.TableReadSessionProvider)
	if !modelOK || !readOK {
		return nil, fmt.Errorf("%w: source engine has no table read session", ErrFieldRecommendationUnsupported)
	}
	path, err := resourcetree.ProviderCatalogPathFromLocator(modelProvider.CatalogModel(), locator)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFieldRecommendationInvalid, err)
	}

	analysisCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	session, err := reader.OpenTableReadSession(analysisCtx, plugin.ConnectionInfo(engine.ConnectionInfo), path, plugin.TableReadSessionOptions{
		Hints: map[string]interface{}{
			format.FieldSelectionOptionKey: format.FieldSelectionOptions{Include: fields},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("%w: open source table: %v", ErrFieldRecommendationUnavailable, err)
	}
	defer session.Close(context.Background())

	accumulators := make(map[string]*decimalRecommendationAccumulator, len(fields))
	for _, field := range fields {
		accumulators[field] = &decimalRecommendationAccumulator{}
	}
	var rowsScanned int64
	validatedFields := false
	for {
		batch, readErr := session.ReadBatch(analysisCtx, 1000)
		if readErr != nil {
			return nil, fmt.Errorf("%w: scan source values: %v", ErrFieldRecommendationUnavailable, readErr)
		}
		if !validatedFields {
			if err := validateDecimalRecommendationFields(batch.Fields, fields); err != nil {
				return nil, err
			}
			validatedFields = true
		}
		if len(batch.Rows) == 0 {
			break
		}
		for _, row := range batch.Rows {
			rowsScanned++
			for _, field := range fields {
				if err := accumulators[field].Add(row[field]); err != nil {
					return nil, fmt.Errorf("%w: field %s: %v", ErrFieldRecommendationUnavailable, field, err)
				}
			}
		}
	}

	result := &FieldDefinitionRecommendationResult{
		TargetEngineType: "mysql",
		Basis:            "exact_source_values",
		RowsScanned:      rowsScanned,
		Fields:           make([]DecimalFieldRecommendation, 0, len(fields)),
	}
	for _, field := range fields {
		precision, scale := accumulators[field].Recommendation()
		result.Fields = append(result.Fields, DecimalFieldRecommendation{
			SourceField: field, Precision: precision, Scale: scale,
			NonNullCount: accumulators[field].NonNullCount,
			FitsTarget:   precision <= 65 && scale <= 30,
		})
	}
	return result, nil
}

func normalizedRecommendationFields(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 64 {
		return nil, fmt.Errorf("%w: source_fields must contain 1 to 64 fields", ErrFieldRecommendationInvalid)
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		field := strings.TrimSpace(value)
		if field == "" {
			return nil, fmt.Errorf("%w: source_fields contains an empty field", ErrFieldRecommendationInvalid)
		}
		if _, exists := seen[field]; exists {
			continue
		}
		seen[field] = struct{}{}
		result = append(result, field)
	}
	return result, nil
}

func validateDecimalRecommendationFields(actual []datatype.FieldInfo, requested []string) error {
	byName := make(map[string]datatype.FieldInfo, len(actual))
	for _, field := range actual {
		byName[field.Name] = field
	}
	for _, name := range requested {
		field, ok := byName[name]
		if !ok || field.Type != datatype.FieldTypeDecimal {
			return fmt.Errorf("%w: source field %s is not decimal", ErrFieldRecommendationInvalid, name)
		}
	}
	return nil
}

type decimalRecommendationAccumulator struct {
	MaxIntegerDigits int
	MaxScale         int
	NonNullCount     int64
}

func (a *decimalRecommendationAccumulator) Add(value interface{}) error {
	if value == nil {
		return nil
	}
	integerDigits, scale, err := decimalValueShape(value)
	if err != nil {
		return err
	}
	a.NonNullCount++
	if integerDigits > a.MaxIntegerDigits {
		a.MaxIntegerDigits = integerDigits
	}
	if scale > a.MaxScale {
		a.MaxScale = scale
	}
	return nil
}

func (a decimalRecommendationAccumulator) Recommendation() (int, int) {
	if a.NonNullCount == 0 {
		return 1, 0
	}
	precision := a.MaxIntegerDigits + a.MaxScale
	if precision < 1 {
		precision = 1
	}
	return precision, a.MaxScale
}

func decimalValueShape(value interface{}) (int, int, error) {
	var text string
	switch typed := value.(type) {
	case []byte:
		text = string(typed)
	case json.Number:
		text = typed.String()
	default:
		text = fmt.Sprint(value)
	}
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "+"), "-"))
	parts := strings.SplitN(strings.ToLower(text), "e", 2)
	mantissa := parts[0]
	exponent := 0
	if len(parts) == 2 {
		parsed, err := strconv.Atoi(parts[1])
		if err != nil || parsed > 100000 || parsed < -100000 {
			return 0, 0, fmt.Errorf("invalid decimal value %q", text)
		}
		exponent = parsed
	}
	mantissaParts := strings.Split(mantissa, ".")
	if len(mantissaParts) > 2 || len(mantissaParts) == 0 {
		return 0, 0, fmt.Errorf("invalid decimal value %q", text)
	}
	integerPart := mantissaParts[0]
	fractionPart := ""
	if len(mantissaParts) == 2 {
		fractionPart = mantissaParts[1]
	}
	digits := integerPart + fractionPart
	if digits == "" || strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return 0, 0, fmt.Errorf("invalid decimal value %q", text)
	}
	leadingZeros := len(digits) - len(strings.TrimLeft(digits, "0"))
	if leadingZeros == len(digits) {
		return 1, 0, nil
	}
	significant := strings.TrimRight(digits[leadingZeros:], "0")
	decimalPoint := len(integerPart) + exponent - leadingZeros
	integerDigits := max(decimalPoint, 0)
	scale := max(len(significant)-decimalPoint, 0)
	return integerDigits, scale, nil
}
